package request

import common "github.com/flipped-aurora/gin-vue-admin/server/model/common/request"

// ---- 平台端: 租户管理 ----

type TenantCreateReq struct {
	Code            string `json:"code" binding:"required,max=64"`
	Name            string `json:"name" binding:"required,max=128"`
	Package         string `json:"package" binding:"max=32"`
	AgentHardLimit  int    `json:"agentHardLimit" binding:"omitempty,min=1,max=8"`
	WelcomeTemplate string `json:"welcomeTemplate" binding:"max=512"`
	DomainWhitelist string `json:"domainWhitelist" binding:"max=512"`
	// 可选: 开通时同步创建首个商户管理员(scope=merchant)账号, 缺省则仅建租户
	AdminUsername string `json:"adminUsername" binding:"max=64"`
	AdminPassword string `json:"adminPassword" binding:"max=64"`
	AdminPhone    string `json:"adminPhone" binding:"max=20"`
	AdminRealName string `json:"adminRealName" binding:"max=64"`
}

type TenantUpdateReq struct {
	ID              uint   `json:"id" binding:"required"`
	Name            string `json:"name" binding:"max=128"`
	Package         string `json:"package" binding:"max=32"`
	AgentHardLimit  int    `json:"agentHardLimit" binding:"omitempty,min=1,max=8"`
	WelcomeTemplate string `json:"welcomeTemplate" binding:"max=512"`
	DomainWhitelist string `json:"domainWhitelist" binding:"max=512"`
}

// TenantStatusReq 启用/停用租户
type TenantStatusReq struct {
	ID     uint `json:"id" binding:"required"`
	Status int  `json:"status" binding:"required,oneof=1 2"`
}

// TenantDeleteReq 注销删除租户(软删租户与其账号, 保留会话审计)
type TenantDeleteReq struct {
	ID uint `json:"id" binding:"required"`
}

type TenantSearchReq struct {
	common.PageInfo
	Status int `json:"status" form:"status"`
}

// ---- 商户端: 客服账号管理 ----

type AgentCreateReq struct {
	Username   string `json:"username" binding:"required,max=64"`
	Password   string `json:"password" binding:"required,min=6,max=64"`
	Phone      string `json:"phone" binding:"required,len=11"`
	RealName   string `json:"realName" binding:"max=64"`
	EmployeeNo string `json:"employeeNo" binding:"max=32"`
	Role       string `json:"role" binding:"required,oneof=agent supervisor"`
	MaxActive  int    `json:"maxActive" binding:"omitempty,min=1,max=8"`
	SkillTags  string `json:"skillTags" binding:"max=256"`
}

type AgentUpdateReq struct {
	ID         uint   `json:"id" binding:"required"`
	Phone      string `json:"phone" binding:"omitempty,len=11"`
	RealName   string `json:"realName" binding:"max=64"`
	EmployeeNo string `json:"employeeNo" binding:"max=32"`
	Role       string `json:"role" binding:"omitempty,oneof=agent supervisor"`
	MaxActive  int    `json:"maxActive" binding:"omitempty,min=1,max=8"`
	SkillTags  string `json:"skillTags" binding:"max=256"`
}

type AgentStatusReq struct {
	ID     uint `json:"id" binding:"required"`
	Enable int  `json:"enable" binding:"required,oneof=1 2"`
}

type AgentResetPwdReq struct {
	ID       uint   `json:"id" binding:"required"`
	Password string `json:"password" binding:"required,min=6,max=64"`
}

type AgentSearchReq struct {
	common.PageInfo
	Role   string `json:"role" form:"role"`
	Enable int    `json:"enable" form:"enable"`
}

// ---- 商户端: 访客(用户端)账号管理 ----

type MemberCreateReq struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,min=6,max=64"`
	NickName string `json:"nickName" binding:"max=64"`
	Phone    string `json:"phone" binding:"omitempty,len=11"`
	Source   string `json:"source" binding:"max=64"`
}

type MemberUpdateReq struct {
	ID       uint   `json:"id" binding:"required"`
	NickName string `json:"nickName" binding:"max=64"`
	Phone    string `json:"phone" binding:"omitempty,len=11"`
	Source   string `json:"source" binding:"max=64"`
	Status   int    `json:"status" binding:"omitempty,oneof=1 2"`
}

type MemberResetPwdReq struct {
	ID       uint   `json:"id" binding:"required"`
	Password string `json:"password" binding:"required,min=6,max=64"`
}

type MemberSearchReq struct {
	common.PageInfo
}

// ---- 商户端: H5 渠道配置 ----

type ChannelConfigReq struct {
	WelcomeTemplate string `json:"welcomeTemplate" binding:"max=512"`
	DomainWhitelist string `json:"domainWhitelist" binding:"max=512"`
}

// ---- 审计查询(平台/商户共用) ----

type AuditSearchReq struct {
	common.PageInfo
	Action   string `json:"action" form:"action"`     // 精确动作码或前缀码(如 tenant / file)
	TenantID uint   `json:"tenantId" form:"tenantId"` // 仅平台侧生效; 商户侧强制本租户
}
