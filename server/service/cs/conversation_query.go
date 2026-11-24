package cs

import (
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
)

// empNoOf 按客服ID取工号(用户侧展示 "客服008")
func empNoOf(agentID uint) string {
	if agentID == 0 {
		return ""
	}
	var a cs.CsAgent
	if err := global.GVA_DB.Select("employee_no").First(&a, agentID).Error; err != nil {
		return ""
	}
	return a.EmployeeNo
}

// ToConversationRes 构造会话响应(用户侧仅暴露工号, 不含真名)
func (s *ConversationService) ToConversationRes(conv *cs.CsConversation) csRes.ConversationRes {
	isFirst := conv.LastAgentID == 0 && conv.FirstAgentAt == nil
	return csRes.ConversationRes{
		ID:             conv.ID,
		TenantID:       conv.TenantID,
		MemberID:       conv.MemberID,
		Title:          conv.Title,
		Status:         conv.Status,
		CurrentAgentID: conv.CurrentAgentID,
		CurrentEmpNo:   empNoOf(conv.CurrentAgentID),
		LastEmpNo:      empNoOf(conv.LastAgentID),
		FirstAgentAt:   conv.FirstAgentAt,
		IsFirstService: isFirst,
		Source:         conv.Source,
		CreatedAt:      conv.CreatedAt,
	}
}

// AttachMemberInfo 批量补充会话响应中的访客用户名(完整)与手机号(脱敏), 仅供客服端列表展示
// 装配失败不阻断列表(退化为无用户名/电话)
func (s *ConversationService) AttachMemberInfo(tenantID uint, list []csRes.ConversationRes) {
	ids := make([]uint, 0, len(list))
	for i := range list {
		if list[i].MemberID != 0 {
			ids = append(ids, list[i].MemberID)
		}
	}
	if len(ids) == 0 {
		return
	}
	var members []cs.CsMember
	if err := global.GVA_DB.Select("id", "username", "nick_name", "phone").
		Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&members).Error; err != nil {
		return
	}
	byID := make(map[uint]cs.CsMember, len(members))
	for _, m := range members {
		byID[m.ID] = m
	}
	for i := range list {
		if m, ok := byID[list[i].MemberID]; ok {
			name := m.NickName
			if name == "" {
				name = m.Username
			}
			list[i].MemberName = name
			list[i].MemberPhone = maskPhone(m.Phone)
		}
	}
}

// ToEventRes 构造接待轨迹响应(客服ID映射为工号)
func (s *ConversationService) ToEventRes(ev *cs.CsAssignmentEvent) csRes.EventRes {
	return csRes.EventRes{
		ID:              ev.ID,
		ConversationID:  ev.ConversationID,
		Action:          ev.Action,
		OperatorAgentID: ev.OperatorAgentID,
		FromAgentID:     ev.FromAgentID,
		ToAgentID:       ev.ToAgentID,
		FromEmpNo:       empNoOf(ev.FromAgentID),
		ToEmpNo:         empNoOf(ev.ToAgentID),
		Reason:          ev.Reason,
		EventTime:       ev.EventTime,
	}
}

// Events 会话接待轨迹(追加记录)
func (s *ConversationService) Events(tenantID, convID uint) ([]csRes.EventRes, error) {
	var list []cs.CsAssignmentEvent
	if err := global.GVA_DB.Where("tenant_id = ? AND conversation_id = ?", tenantID, convID).
		Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]csRes.EventRes, 0, len(list))
	for i := range list {
		out = append(out, s.ToEventRes(&list[i]))
	}
	return out, nil
}

// TransferableAgents 同租户可转接客服列表(id/工号/姓名/角色, 不含敏感字段)
type TransferableAgentRes struct {
	ID         uint   `json:"id"`
	EmployeeNo string `json:"employeeNo"`
	RealName   string `json:"realName"`
	Role       string `json:"role"`
}

func (s *ConversationService) TransferableAgents(tenantID, selfAgentID uint) ([]TransferableAgentRes, error) {
	var list []cs.CsAgent
	if err := global.GVA_DB.Where("tenant_id = ? AND enable = 1 AND id <> ?", tenantID, selfAgentID).
		Select("id", "employee_no", "real_name", "role").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]TransferableAgentRes, 0, len(list))
	for _, a := range list {
		out = append(out, TransferableAgentRes{ID: a.ID, EmployeeNo: a.EmployeeNo, RealName: a.RealName, Role: a.Role})
	}
	return out, nil
}

// UserSidebar 客服工作台侧栏用户资料(最小权限: 姓名/脱敏电话/来源) + 跨会话历史接待汇总
func (s *ConversationService) UserSidebar(tenantID, memberID uint) (*csRes.UserSidebarRes, error) {
	var m cs.CsMember
	if err := global.GVA_DB.Where("id = ? AND tenant_id = ?", memberID, tenantID).First(&m).Error; err != nil {
		return nil, ErrConvNotFound
	}
	res := &csRes.UserSidebarRes{
		MemberID:  m.ID,
		Name:      m.NickName,
		Phone:     maskPhone(m.Phone),
		Source:    m.Source,
		TenantID:  m.TenantID,
		CreatedAt: m.CreatedAt,
	}
	// 接待汇总: 该用户全部会话数 + 历史接待过的客服(首次接待时间), 仅限同租户
	var total int64
	_ = global.GVA_DB.Model(&cs.CsConversation{}).Where("tenant_id = ? AND member_id = ?", tenantID, memberID).Count(&total).Error
	type serverRow struct {
		ToAgentID uint
		ServedAt  time.Time
	}
	var rows []serverRow
	sql := `SELECT e.to_agent_id, MIN(e.event_time) AS served_at
		FROM cs_assignment_events e JOIN cs_conversations c ON c.id = e.conversation_id
		WHERE c.tenant_id = ? AND c.member_id = ? AND e.action IN ('assign','transfer') AND e.to_agent_id > 0
		GROUP BY e.to_agent_id ORDER BY served_at`
	if err := global.GVA_DB.Raw(sql, tenantID, memberID).Scan(&rows).Error; err == nil {
		summary := &csRes.MemberServiceSummary{TotalConversations: total, Servers: make([]csRes.ServerContactRes, 0, len(rows))}
		for _, r := range rows {
			if no := empNoOf(r.ToAgentID); no != "" {
				summary.Servers = append(summary.Servers, csRes.ServerContactRes{EmployeeNo: no, ServedAt: r.ServedAt.UTC()})
			}
		}
		res.Summary = summary
	}
	return res, nil
}
