package cs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
	"gorm.io/gorm"
)

var (
	ErrTenantCodeExists  = errors.New("租户编码已存在")
	ErrUsernameExists    = errors.New("登录名已被占用")
	ErrCannotSelfDisable = errors.New("不能停用自己")
)

// ---- 平台端: 租户管理 ----

type TenantAdminService struct{}

func (s *TenantAdminService) Create(req csReq.TenantCreateReq) (*csRes.TenantRes, error) {
	hard := req.AgentHardLimit
	if hard <= 0 || hard > agentHardCap {
		hard = agentHardCap
	}
	t := cs.CsTenant{
		Code: strings.TrimSpace(req.Code), Name: strings.TrimSpace(req.Name),
		Status: cs.TenantStatusNormal, Package: req.Package,
		AgentHardLimit: hard, WelcomeTemplate: req.WelcomeTemplate,
		DomainWhitelist: req.DomainWhitelist,
	}
	// 同步开通首个商户管理员: 没有管理员的新商户将无法进后台
	var admin *cs.CsAgent
	if uname := strings.TrimSpace(req.AdminUsername); uname != "" {
		if !ValidPhone(req.AdminPhone) {
			return nil, errors.New("商户管理员需绑定有效11位手机号(登录接收短信验证码)")
		}
		if len(req.AdminPassword) < 6 {
			return nil, errors.New("商户管理员密码至少6位")
		}
		hash, err := HashPassword(req.AdminPassword)
		if err != nil {
			return nil, err
		}
		admin = &cs.CsAgent{
			Username: uname, Password: hash, Phone: req.AdminPhone,
			RealName: req.AdminRealName, Role: cs.AgentRoleMerchantAdmin,
			Enable: cs.AccountStatusNormal,
		}
	}
	err := global.GVA_DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		if admin != nil {
			admin.TenantID = t.ID
			return tx.Create(admin).Error
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			// 租户已建成功后再报冲突 = 管理员登录名全局重复
			if t.ID != 0 {
				return nil, ErrUsernameExists
			}
			return nil, ErrTenantCodeExists
		}
		return nil, err
	}
	return toTenantRes(&t, 0), nil
}

func (s *TenantAdminService) Update(req csReq.TenantUpdateReq) error {
	var t cs.CsTenant
	if err := global.GVA_DB.First(&t, req.ID).Error; err != nil {
		return ErrTenantNotFound
	}
	hard := req.AgentHardLimit
	if hard <= 0 || hard > agentHardCap {
		hard = t.AgentHardLimit
	}
	return global.GVA_DB.Model(&t).Updates(map[string]interface{}{
		"name": req.Name, "package": req.Package, "agent_hard_limit": hard,
		"welcome_template": req.WelcomeTemplate, "domain_whitelist": req.DomainWhitelist,
	}).Error
}

func (s *TenantAdminService) SetStatus(req csReq.TenantStatusReq) error {
	res := global.GVA_DB.Model(&cs.CsTenant{}).Where("id = ?", req.ID).Update("status", req.Status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTenantNotFound
	}
	return nil
}

// Delete 注销商户: 软删租户及其客服/访客账号; 存在进行中会话则拒绝(保留历史会话审计)
func (s *TenantAdminService) Delete(id uint) error {
	var running int64
	if err := global.GVA_DB.Model(&cs.CsConversation{}).
		Where("tenant_id = ? AND status IN ?", id, []string{cs.ConvStatusPending, cs.ConvStatusActive}).
		Count(&running).Error; err != nil {
		return err
	}
	if running > 0 {
		return fmt.Errorf("该商户仍有 %d 个未结束会话, 请先停用并结束全部会话", running)
	}
	return global.GVA_DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&cs.CsTenant{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrTenantNotFound
		}
		if err := tx.Where("tenant_id = ?", id).Delete(&cs.CsAgent{}).Error; err != nil {
			return err
		}
		return tx.Where("tenant_id = ?", id).Delete(&cs.CsMember{}).Error
	})
}

func (s *TenantAdminService) List(req csReq.TenantSearchReq) ([]csRes.TenantRes, int64, error) {
	db := global.GVA_DB.Model(&cs.CsTenant{})
	if req.Status != 0 {
		db = db.Where("status = ?", req.Status)
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("name LIKE ? OR code LIKE ?", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []cs.CsTenant
	if err := db.Scopes(req.Paginate()).Order("id DESC").Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]csRes.TenantRes, 0, len(list))
	for i := range list {
		var cnt int64
		global.GVA_DB.Model(&cs.CsAgent{}).Where("tenant_id = ?", list[i].ID).Count(&cnt)
		out = append(out, *toTenantRes(&list[i], cnt))
	}
	return out, total, nil
}

func toTenantRes(t *cs.CsTenant, agentCount int64) *csRes.TenantRes {
	return &csRes.TenantRes{
		ID: t.ID, Code: t.Code, Name: t.Name, Status: t.Status, Package: t.Package,
		AgentHardLimit: t.AgentHardLimit, WelcomeTemplate: t.WelcomeTemplate,
		DomainWhitelist: t.DomainWhitelist, AgentCount: agentCount, CreatedAt: t.CreatedAt,
	}
}

// ---- 商户端: 客服账号管理(强制租户作用域) ----

type AgentAdminService struct{}

func (s *AgentAdminService) Create(tenantID uint, req csReq.AgentCreateReq) (*csRes.AgentRes, error) {
	var tenant cs.CsTenant
	if err := global.GVA_DB.First(&tenant, tenantID).Error; err != nil {
		return nil, ErrTenantNotFound
	}
	pwd, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	maxActive := req.MaxActive
	if maxActive <= 0 || maxActive > agentHardCap {
		maxActive = tenant.AgentHardLimit
		if maxActive > agentHardCap {
			maxActive = agentHardCap
		}
	}
	empNo := strings.TrimSpace(req.EmployeeNo)
	if empNo == "" {
		empNo = genEmployeeNo()
	}
	a := cs.CsAgent{
		TenantID: tenantID, Username: strings.TrimSpace(req.Username), Password: pwd,
		Phone: req.Phone, RealName: req.RealName, EmployeeNo: empNo,
		Role: req.Role, Enable: cs.AccountStatusNormal, MaxActive: maxActive, SkillTags: req.SkillTags,
	}
	if err := global.GVA_DB.Create(&a).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameExists
		}
		return nil, err
	}
	return toAgentRes(&a), nil
}

// Update 仅允许操作本租户账号(WHERE id AND tenant_id)
func (s *AgentAdminService) Update(tenantID uint, req csReq.AgentUpdateReq) error {
	var a cs.CsAgent
	if err := global.GVA_DB.Where("id = ? AND tenant_id = ?", req.ID, tenantID).First(&a).Error; err != nil {
		return ErrAccountNotFound
	}
	updates := map[string]interface{}{}
	if req.Phone != "" {
		updates["phone"] = req.Phone
	}
	if req.RealName != "" {
		updates["real_name"] = req.RealName
	}
	if req.EmployeeNo != "" {
		updates["employee_no"] = req.EmployeeNo
	}
	if req.Role != "" {
		updates["role"] = req.Role
	}
	if req.MaxActive > 0 {
		v := req.MaxActive
		if v > agentHardCap {
			v = agentHardCap
		}
		updates["max_active"] = v
	}
	if req.SkillTags != "" {
		updates["skill_tags"] = req.SkillTags
	}
	if len(updates) == 0 {
		return nil
	}
	return global.GVA_DB.Model(&a).Updates(updates).Error
}

func (s *AgentAdminService) SetStatus(tenantID, operatorID uint, req csReq.AgentStatusReq) error {
	if operatorID == req.ID && req.Enable != cs.AccountStatusNormal {
		return ErrCannotSelfDisable
	}
	res := global.GVA_DB.Model(&cs.CsAgent{}).
		Where("id = ? AND tenant_id = ?", req.ID, tenantID).
		Update("enable", req.Enable)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *AgentAdminService) ResetPassword(tenantID uint, req csReq.AgentResetPwdReq) error {
	pwd, err := HashPassword(req.Password)
	if err != nil {
		return err
	}
	res := global.GVA_DB.Model(&cs.CsAgent{}).
		Where("id = ? AND tenant_id = ?", req.ID, tenantID).
		Update("password", pwd)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *AgentAdminService) List(tenantID uint, req csReq.AgentSearchReq) ([]csRes.AgentRes, int64, error) {
	db := global.GVA_DB.Model(&cs.CsAgent{}).Where("tenant_id = ?", tenantID)
	if req.Role != "" {
		db = db.Where("role = ?", req.Role)
	}
	if req.Enable != 0 {
		db = db.Where("enable = ?", req.Enable)
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("username LIKE ? OR real_name LIKE ? OR employee_no LIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []cs.CsAgent
	if err := db.Scopes(req.Paginate()).Order("id DESC").Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]csRes.AgentRes, 0, len(list))
	for i := range list {
		out = append(out, *toAgentRes(&list[i]))
	}
	return out, total, nil
}

// refreshCurrentCount 用事务重算某客服进行中会话数快照(权威值仍是 Claim/Transfer 事务)
func (s *AgentAdminService) refreshCurrentCount(tenantID, agentID uint) (int64, error) {
	var cnt int64
	err := global.GVA_DB.Model(&cs.CsConversation{}).
		Where("tenant_id = ? AND current_agent_id = ? AND status = ?", tenantID, agentID, cs.ConvStatusActive).
		Count(&cnt).Error
	if err == nil {
		global.GVA_DB.Model(&cs.CsAgent{}).Where("id = ? AND tenant_id = ?", agentID, tenantID).
			Update("current_count", cnt)
	}
	return cnt, err
}

func toAgentRes(a *cs.CsAgent) *csRes.AgentRes {
	return &csRes.AgentRes{
		ID: a.ID, Username: a.Username, Phone: a.Phone, RealName: a.RealName,
		EmployeeNo: a.EmployeeNo, Role: a.Role, Enable: a.Enable, MaxActive: a.MaxActive,
		SkillTags: a.SkillTags, CurrentCount: a.CurrentCount, CreatedAt: a.CreatedAt,
	}
}

// ---- 商户端: H5 渠道配置(写在租户上的欢迎语/域名白名单) ----

type ChannelService struct{}

func (s *ChannelService) GetConfig(tenantID uint) (*csRes.TenantRes, error) {
	var t cs.CsTenant
	if err := global.GVA_DB.First(&t, tenantID).Error; err != nil {
		return nil, ErrTenantNotFound
	}
	return toTenantRes(&t, 0), nil
}

func (s *ChannelService) UpdateConfig(tenantID uint, req csReq.ChannelConfigReq) error {
	return global.GVA_DB.Model(&cs.CsTenant{}).Where("id = ?", tenantID).Updates(map[string]interface{}{
		"welcome_template": req.WelcomeTemplate, "domain_whitelist": req.DomainWhitelist,
	}).Error
}

// ---- 商户端: 报表 ----

type ReportService struct{}

func (s *ReportService) Overview(tenantID uint) (*csRes.OverviewRes, error) {
	res := &csRes.OverviewRes{TenantID: tenantID}
	global.GVA_DB.Model(&cs.CsAgent{}).Where("tenant_id = ?", tenantID).Count(&res.AgentTotal)

	convDB := func(status string) *gorm.DB {
		return global.GVA_DB.Model(&cs.CsConversation{}).Where("tenant_id = ? AND status = ?", tenantID, status)
	}
	convDB(cs.ConvStatusPending).Count(&res.ConvPending)
	convDB(cs.ConvStatusActive).Count(&res.ConvActive)
	global.GVA_DB.Model(&cs.CsConversation{}).Where("tenant_id = ?", tenantID).Count(&res.ConvTotal)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	global.GVA_DB.Model(&cs.CsConversation{}).
		Where("tenant_id = ? AND status = ? AND closed_at >= ?", tenantID, cs.ConvStatusClosed, today).
		Count(&res.ConvClosedToday)

	// 在线客服 = 有进行中会话的去重客服数
	global.GVA_DB.Model(&cs.CsConversation{}).
		Where("tenant_id = ? AND status = ? AND current_agent_id > 0", tenantID, cs.ConvStatusActive).
		Distinct("current_agent_id").Count(&res.AgentOnline)
	return res, nil
}

func (s *ReportService) AgentPerformance(tenantID uint) ([]csRes.AgentPerfRes, error) {
	var agents []cs.CsAgent
	if err := global.GVA_DB.Where("tenant_id = ?", tenantID).Find(&agents).Error; err != nil {
		return nil, err
	}
	out := make([]csRes.AgentPerfRes, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		per := csRes.AgentPerfRes{AgentID: a.ID, EmployeeNo: a.EmployeeNo, RealName: a.RealName}
		// served: 该客服作为转入/接入目标的轨迹事件数(assign + transfer 到他)
		global.GVA_DB.Model(&cs.CsAssignmentEvent{}).
			Where("tenant_id = ? AND to_agent_id = ? AND action IN ?", tenantID, a.ID, []string{cs.EventAssign, cs.EventTransfer}).
			Count(&per.Served)
		global.GVA_DB.Model(&cs.CsConversation{}).
			Where("tenant_id = ? AND current_agent_id = ? AND status = ?", tenantID, a.ID, cs.ConvStatusActive).
			Count(&per.Active)
		global.GVA_DB.Model(&cs.CsConversation{}).
			Where("tenant_id = ? AND last_agent_id = ? AND status = ?", tenantID, a.ID, cs.ConvStatusClosed).
			Count(&per.Closed)
		out = append(out, per)
	}
	return out, nil
}

// isUniqueViolation 兼容 pgsql(23xxx)/mysql(1062) 唯一约束冲突判定
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "1062") || strings.Contains(msg, "duplicate entry")
}

// genEmployeeNo 生成 4 位工号兜底(商户可后续改)
func genEmployeeNo() string {
	var maxID int64
	global.GVA_DB.Model(&cs.CsAgent{}).Select("COALESCE(MAX(id),0)").Scan(&maxID)
	return fmt.Sprintf("E%04d", maxID+1)
}

// PlatformReportService 平台聚合运营指标(方案 5.3)
type PlatformReportService struct{}

func (s *PlatformReportService) Overview(ctx context.Context) (*csRes.PlatformOverviewRes, error) {
	res := &csRes.PlatformOverviewRes{}
	if err := global.GVA_DB.Model(&cs.CsTenant{}).Count(&res.TenantTotal).Error; err != nil {
		return nil, err
	}
	global.GVA_DB.Model(&cs.CsTenant{}).Where("status = ?", cs.TenantStatusNormal).Count(&res.TenantActive)
	global.GVA_DB.Model(&cs.CsConversation{}).Count(&res.ConvTotal)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	global.GVA_DB.Model(&cs.CsConversation{}).Where("created_at >= ?", today).Count(&res.ConvToday)
	global.GVA_DB.Model(&cs.CsConversation{}).Where("status = ?", cs.ConvStatusPending).Count(&res.ConvPending)
	var online int64
	global.GVA_DB.Model(&cs.CsConversation{}).Where("status = ?", cs.ConvStatusActive).Distinct("current_agent_id").Count(&online)
	res.OnlineAgents = online
	res.MsgTotal = SumMetric(ctx, MetricMsgSent)
	return res, nil
}

// ---- 商户端: 访客(用户端)账号管理, 严格限本租户 ----

type MemberAdminService struct{}

func (s *MemberAdminService) List(tenantID uint, req csReq.MemberSearchReq) ([]csRes.MemberRes, int64, error) {
	db := global.GVA_DB.Model(&cs.CsMember{}).Where("tenant_id = ?", tenantID)
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("username LIKE ? OR nick_name LIKE ? OR phone LIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []cs.CsMember
	if err := db.Scopes(req.Paginate()).Order("id DESC").Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]csRes.MemberRes, 0, len(list))
	for i := range list {
		out = append(out, toMemberRes(&list[i]))
	}
	return out, total, nil
}

func (s *MemberAdminService) Create(tenantID uint, req csReq.MemberCreateReq) (*csRes.MemberRes, error) {
	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	m := cs.CsMember{
		TenantID: tenantID, Username: strings.TrimSpace(req.Username), Password: hash,
		NickName: req.NickName, Phone: req.Phone, Source: req.Source,
		Status: cs.AccountStatusNormal,
	}
	if err := global.GVA_DB.Create(&m).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameExists
		}
		return nil, err
	}
	res := toMemberRes(&m)
	return &res, nil
}

func (s *MemberAdminService) Update(tenantID uint, req csReq.MemberUpdateReq) error {
	updates := map[string]interface{}{
		"nick_name": req.NickName, "phone": req.Phone, "source": req.Source,
	}
	if req.Status != 0 {
		updates["status"] = req.Status
	}
	res := global.GVA_DB.Model(&cs.CsMember{}).Where("id = ? AND tenant_id = ?", req.ID, tenantID).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *MemberAdminService) ResetPwd(tenantID uint, req csReq.MemberResetPwdReq) error {
	hash, err := HashPassword(req.Password)
	if err != nil {
		return err
	}
	res := global.GVA_DB.Model(&cs.CsMember{}).Where("id = ? AND tenant_id = ?", req.ID, tenantID).Update("password", hash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func toMemberRes(m *cs.CsMember) csRes.MemberRes {
	return csRes.MemberRes{
		ID: m.ID, Username: m.Username, NickName: m.NickName, Phone: m.Phone,
		Source: m.Source, Status: m.Status, CreatedAt: m.CreatedAt,
	}
}
