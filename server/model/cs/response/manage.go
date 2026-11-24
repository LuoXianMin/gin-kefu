package response

import "time"

// TenantPublicRes H5 渠道公开的商户信息(仅展示字段, 不泄漏配置/域名白名单)
type TenantPublicRes struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	Status          int    `json:"status"`
	WelcomeTemplate string `json:"welcomeTemplate"`
}

// MemberRes 商户后台可见的访客账号
type MemberRes struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	NickName  string    `json:"nickName"`
	Phone     string    `json:"phone"`
	Source    string    `json:"source"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// TenantRes 平台端租户列表/详情项
type TenantRes struct {
	ID              uint      `json:"id"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	Status          int       `json:"status"`
	Package         string    `json:"package"`
	AgentHardLimit  int       `json:"agentHardLimit"`
	WelcomeTemplate string    `json:"welcomeTemplate"`
	DomainWhitelist string    `json:"domainWhitelist"`
	AgentCount      int64     `json:"agentCount"`
	CreatedAt       time.Time `json:"createdAt"`
}

// AgentRes 商户端客服账号项(商户管理员可见真名; 不含密码)
type AgentRes struct {
	ID           uint      `json:"id"`
	Username     string    `json:"username"`
	Phone        string    `json:"phone"`
	RealName     string    `json:"realName"`
	EmployeeNo   string    `json:"employeeNo"`
	Role         string    `json:"role"`
	Enable       int       `json:"enable"`
	MaxActive    int       `json:"maxActive"`
	SkillTags    string    `json:"skillTags"`
	CurrentCount int       `json:"currentCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

// OverviewRes 商户端接待总览报表
type OverviewRes struct {
	TenantID        uint  `json:"tenantId"`
	AgentTotal      int64 `json:"agentTotal"`
	AgentOnline     int64 `json:"agentOnline"` // 有进行中会话的客服数
	ConvPending     int64 `json:"convPending"`
	ConvActive      int64 `json:"convActive"`
	ConvClosedToday int64 `json:"convClosedToday"`
	ConvTotal       int64 `json:"convTotal"`
}

// AgentPerfRes 单个客服接待绩效
type AgentPerfRes struct {
	AgentID    uint   `json:"agentId"`
	EmployeeNo string `json:"employeeNo"`
	RealName   string `json:"realName"`
	Served     int64  `json:"served"` // 累计接待(成为 current_agent 次数)
	Active     int64  `json:"active"` // 进行中
	Closed     int64  `json:"closed"` // 已结束
}

// PlatformOverviewRes 平台聚合运营指标(方案 5.3)
type PlatformOverviewRes struct {
	TenantTotal  int64 `json:"tenantTotal"`
	TenantActive int64 `json:"tenantActive"` // 状态正常的租户数
	ConvTotal    int64 `json:"convTotal"`
	ConvToday    int64 `json:"convToday"`    // 今日(UTC)新建会话
	ConvPending  int64 `json:"convPending"`  // 当前排队中
	MsgTotal     int64 `json:"msgTotal"`     // 累计消息量(Redis 计数聚合)
	OnlineAgents int64 `json:"onlineAgents"` // 有进行中会话的客服数
}
