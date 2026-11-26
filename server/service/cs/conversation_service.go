package cs

import (
	"context"
	"errors"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ConversationService 会话接待用例(共享领域服务, 各端调用带独立鉴权)
type ConversationService struct{}

var (
	ErrConvNotFound    = errors.New("会话不存在或无权访问")
	ErrConvNotPending  = errors.New("会话已被接入或已结束")
	ErrAgentAtCapacity = errors.New("客服接待数已达上限")
	ErrNotOwner        = errors.New("非本会话接待客服")
	ErrTargetInvalid   = errors.New("转接目标客服无效")
	ErrAlreadyFirst    = errors.New("会话正在接待中")
)

// CreateOrResume 用户登录后创建或恢复未结束会话(服务端建立成员关系)
func (s *ConversationService) CreateOrResume(tenantID, memberID uint, source, title string) (*cs.CsConversation, error) {
	var conv cs.CsConversation
	err := global.GVA_DB.Where("tenant_id = ? AND member_id = ? AND status IN ?", tenantID, memberID,
		[]string{cs.ConvStatusPending, cs.ConvStatusActive}).
		Order("id desc").First(&conv).Error
	if err == nil {
		return &conv, nil // 恢复未结束会话
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	conv = cs.CsConversation{
		TenantID: tenantID, MemberID: memberID, Status: cs.ConvStatusPending,
		Source: source, Title: title,
	}
	err = global.GVA_DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&conv).Error; e != nil {
			return e
		}
		member := cs.CsConversationMember{TenantID: tenantID, ConversationID: conv.ID, Scope: cs.ScopeMember, AccountID: memberID}
		return tx.Create(&member).Error
	})
	if err != nil {
		return nil, err
	}
	EmitConvEvent(&ConvEvent{TenantID: tenantID, ConversationID: conv.ID, Op: "created",
		Status: cs.ConvStatusPending, MemberID: memberID})
	// 新会话自动发送商户配置的机器人欢迎语(失败仅记日志, 不影响建会话)
	var tenant cs.CsTenant
	if e := global.GVA_DB.Select("welcome_template").First(&tenant, tenantID).Error; e == nil && tenant.WelcomeTemplate != "" {
		if _, e = (&MessageService{}).SendWelcome(context.Background(), tenantID, &conv, tenant.WelcomeTemplate); e != nil {
			global.GVA_LOG.Warn("welcome message send failed", zap.Uint("convID", conv.ID), zap.Error(e))
		}
	}
	return &conv, nil
}

// Claim 原子抢单: 行锁保证同会话仅一人接入, 事务内校验客服接待数不超过 min(8, maxActive)
func (s *ConversationService) Claim(tenantID, agentID, convID uint) (*cs.CsConversation, error) {
	var result cs.CsConversation
	var agentNo string
	err := global.GVA_DB.Transaction(func(tx *gorm.DB) error {
		var agent cs.CsAgent
		if e := tx.Where("id = ? AND tenant_id = ?", agentID, tenantID).First(&agent).Error; e != nil {
			return ErrTargetInvalid
		}
		limit := agent.MaxActive
		if limit <= 0 || limit > agentHardCap {
			limit = agentHardCap
		}

		// 锁住会话行, 串行化同会话抢单
		var conv cs.CsConversation
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", convID, tenantID).First(&conv).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return ErrConvNotFound
			}
			return e
		}
		if conv.Status != cs.ConvStatusPending {
			return ErrConvNotPending
		}

		// 客服当前接待数(权威: 统计 active 且 current_agent=自己)
		var active int64
		if e := tx.Model(&cs.CsConversation{}).
			Where("tenant_id = ? AND current_agent_id = ? AND status = ?", tenantID, agentID, cs.ConvStatusActive).
			Count(&active).Error; e != nil {
			return e
		}
		if active >= int64(limit) {
			return ErrAgentAtCapacity
		}

		now := time.Now().UTC()
		updates := map[string]interface{}{
			"status":           cs.ConvStatusActive,
			"current_agent_id": agentID,
		}
		if conv.FirstAgentAt == nil {
			updates["first_agent_at"] = &now
		}
		if e := tx.Model(&conv).Updates(updates).Error; e != nil {
			return e
		}
		// 成员关系: 客服加入
		cm := cs.CsConversationMember{TenantID: tenantID, ConversationID: conv.ID, Scope: cs.ScopeAgent, AccountID: agentID}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cm).Error; e != nil {
			return e
		}
		if e := s.appendEvent(tx, tenantID, conv.ID, agentID, cs.EventAssign, 0, agentID, "", ""); e != nil {
			return e
		}
		agentNo = agent.EmployeeNo
		result = conv
		result.Status = cs.ConvStatusActive
		result.CurrentAgentID = agentID
		return nil
	})
	if err != nil {
		return nil, err
	}
	EmitConvEvent(&ConvEvent{TenantID: tenantID, ConversationID: result.ID, Op: "claimed",
		Status: cs.ConvStatusActive, AgentID: agentID, AgentEmployeeNo: agentNo})
	return &result, nil
}

// Transfer 转接: 原客服失去接待权, 新客服获得, 写入轨迹
func (s *ConversationService) Transfer(tenantID, fromAgentID, convID, toAgentID uint, reason string) error {
	var toNo string
	err := global.GVA_DB.Transaction(func(tx *gorm.DB) error {
		var conv cs.CsConversation
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", convID, tenantID).First(&conv).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return ErrConvNotFound
			}
			return e
		}
		if conv.CurrentAgentID != fromAgentID {
			return ErrNotOwner
		}
		if conv.Status != cs.ConvStatusActive {
			return ErrConvNotPending
		}
		var to cs.CsAgent
		if e := tx.Where("id = ? AND tenant_id = ? AND enable = ? AND role IN ?",
			toAgentID, tenantID, cs.AccountStatusNormal, []string{cs.AgentRoleAgent, cs.AgentRoleSupervisor}).
			First(&to).Error; e != nil {
			return ErrTargetInvalid
		}
		limit := to.MaxActive
		if limit <= 0 || limit > agentHardCap {
			limit = agentHardCap
		}
		var toActive int64
		tx.Model(&cs.CsConversation{}).Where("tenant_id = ? AND current_agent_id = ? AND status = ?",
			tenantID, toAgentID, cs.ConvStatusActive).Count(&toActive)
		if toActive >= int64(limit) {
			return ErrAgentAtCapacity
		}

		now := time.Now().UTC()
		if e := tx.Model(&conv).Updates(map[string]interface{}{
			"current_agent_id": toAgentID,
			"last_agent_id":    fromAgentID,
			"last_agent_at":    &now,
		}).Error; e != nil {
			return e
		}
		cm := cs.CsConversationMember{TenantID: tenantID, ConversationID: conv.ID, Scope: cs.ScopeAgent, AccountID: toAgentID}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cm).Error; e != nil {
			return e
		}
		if e := s.appendEvent(tx, tenantID, conv.ID, fromAgentID, cs.EventTransfer, fromAgentID, toAgentID, reason, ""); e != nil {
			return e
		}
		toNo = to.EmployeeNo
		return nil
	})
	if err != nil {
		return err
	}
	EmitConvEvent(&ConvEvent{TenantID: tenantID, ConversationID: convID, Op: "transferred",
		Status: cs.ConvStatusActive, AgentID: toAgentID, AgentEmployeeNo: toNo})
	return nil
}

// Close 结束会话
func (s *ConversationService) Close(tenantID, agentID, convID uint, reason string) error {
	var memberID uint
	err := global.GVA_DB.Transaction(func(tx *gorm.DB) error {
		var conv cs.CsConversation
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", convID, tenantID).First(&conv).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return ErrConvNotFound
			}
			return e
		}
		if conv.CurrentAgentID != agentID {
			return ErrNotOwner
		}
		if conv.Status != cs.ConvStatusActive {
			return ErrAlreadyFirst
		}
		now := time.Now().UTC()
		if e := tx.Model(&conv).Updates(map[string]interface{}{
			"status": cs.ConvStatusClosed, "closed_at": &now,
			"last_agent_id": agentID, "last_agent_at": &now,
		}).Error; e != nil {
			return e
		}
		if e := s.appendEvent(tx, tenantID, conv.ID, agentID, cs.EventClose, agentID, 0, reason, ""); e != nil {
			return e
		}
		memberID = conv.MemberID
		return nil
	})
	if err != nil {
		return err
	}
	EmitConvEvent(&ConvEvent{TenantID: tenantID, ConversationID: convID, Op: "closed",
		Status: cs.ConvStatusClosed, MemberID: memberID, AgentID: agentID})
	return nil
}

func (s *ConversationService) appendEvent(tx *gorm.DB, tenantID, convID, operator uint, action string, from, to uint, reason, requestID string) error {
	ev := cs.CsAssignmentEvent{
		TenantID: tenantID, ConversationID: convID, OperatorAgentID: operator,
		Action: action, FromAgentID: from, ToAgentID: to, Reason: reason,
		RequestID: requestID, EventTime: time.Now().UTC(),
	}
	return tx.Create(&ev).Error
}

// ListPending 待接入列表(本租户)
func (s *ConversationService) ListPending(tenantID uint, limit, offset int) ([]cs.CsConversation, int64, error) {
	var (
		list  []cs.CsConversation
		total int64
	)
	q := global.GVA_DB.Model(&cs.CsConversation{}).Where("tenant_id = ? AND status = ?", tenantID, cs.ConvStatusPending)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id asc").Limit(limit).Offset(offset).Find(&list).Error
	return list, total, err
}

// CanAccess 校验账号是否为会话成员(客服归属/会话成员关系), 防止切换会话 ID 越权
func (s *ConversationService) CanAccess(tenantID uint, scope string, accountID, convID uint) (*cs.CsConversation, bool) {
	var conv cs.CsConversation
	if err := global.GVA_DB.Where("id = ? AND tenant_id = ?", convID, tenantID).First(&conv).Error; err != nil {
		return nil, false
	}
	var cnt int64
	global.GVA_DB.Model(&cs.CsConversationMember{}).
		Where("conversation_id = ? AND scope = ? AND account_id = ?", convID, scope, accountID).Count(&cnt)
	if cnt == 0 {
		return nil, false
	}
	return &conv, true
}

const agentHardCap = 8

// ListByAgent 我接待中的会话(active)
func (s *ConversationService) ListByAgent(tenantID, agentID uint) ([]cs.CsConversation, int64, error) {
	var (
		list  []cs.CsConversation
		total int64
	)
	q := global.GVA_DB.Model(&cs.CsConversation{}).
		Where("tenant_id = ? AND current_agent_id = ? AND status = ?", tenantID, agentID, cs.ConvStatusActive)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id desc").Limit(50).Find(&list).Error
	return list, total, err
}
