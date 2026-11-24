package cs

import (
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// CsTenant 企业租户(商户)
type CsTenant struct {
	global.GVA_MODEL
	Code            string `json:"code" gorm:"uniqueIndex;size:64;not null;comment:租户编码"`
	Name            string `json:"name" gorm:"size:128;not null;comment:租户名称"`
	Status          int    `json:"status" gorm:"default:1;comment:1正常 2停用"`
	Package         string `json:"package" gorm:"size:32;comment:套餐"`
	AgentHardLimit  int    `json:"agentHardLimit" gorm:"default:8;comment:系统硬上限固定8"`
	WelcomeTemplate string `json:"welcomeTemplate" gorm:"size:512;comment:默认欢迎语模板"`
	DomainWhitelist string `json:"domainWhitelist" gorm:"size:512;comment:H5渠道域名白名单,逗号分隔"`
}

func (CsTenant) TableName() string { return "cs_tenants" }

const (
	TenantStatusNormal = 1
	TenantStatusClosed = 2
)

// CsAgent 客服/商户管理员账号 商户端与客服端登录主体
type CsAgent struct {
	global.GVA_MODEL
	TenantID     uint   `json:"tenantId" gorm:"index;not null;comment:租户ID"`
	Username     string `json:"username" gorm:"uniqueIndex;size:64;not null;comment:登录名"`
	Password     string `json:"-" gorm:"size:128;not null;comment:bcrypt密码"`
	Phone        string `json:"phone" gorm:"size:20;index;comment:手机号(短信验证码接收)"`
	RealName     string `json:"realName" gorm:"size:64;comment:真名(仅商户管理可见,用户端不返回)"`
	EmployeeNo   string `json:"employeeNo" gorm:"size:32;comment:工号,用户侧展示如 客服008"`
	Role         string `json:"role" gorm:"size:20;comment:merchant_admin|agent|supervisor"`
	Enable       int    `json:"enable" gorm:"default:1;comment:1正常 2禁用"`
	MaxActive    int    `json:"maxActive" gorm:"default:8;comment:接待上限,硬上限8"`
	SkillTags    string `json:"skillTags" gorm:"size:256;comment:技能标签"`
	TeamID       uint   `json:"teamId" gorm:"index;comment:团队ID(D期扩展)"`
	CurrentCount int    `json:"currentCount" gorm:"default:0;comment:进行中会话数快照(权威以D期事务为准)"`
}

func (CsAgent) TableName() string { return "cs_agents" }

const (
	AgentRoleMerchantAdmin = "merchant_admin"
	AgentRoleAgent         = "agent"
	AgentRoleSupervisor    = "supervisor"

	AccountStatusNormal = 1
	AccountStatusClosed = 2
)

// CsMember 用户端(H5)账号
type CsMember struct {
	global.GVA_MODEL
	TenantID uint   `json:"tenantId" gorm:"index;not null;comment:租户ID"`
	Username string `json:"username" gorm:"uniqueIndex;size:64;not null;comment:登录名"`
	Password string `json:"-" gorm:"size:128;not null;comment:bcrypt密码"`
	NickName string `json:"nickName" gorm:"size:64;comment:昵称"`
	Phone    string `json:"phone" gorm:"size:20;comment:手机号"`
	Source   string `json:"source" gorm:"size:64;comment:来源渠道(H5/落地页/业务入口)"`
	Status   int    `json:"status" gorm:"default:1;comment:1正常 2停用"`
}

func (CsMember) TableName() string { return "cs_members" }

// CsLoginFailure 登录失败计数(持久化兜底, Redis可用时优先走Redis)
type CsLoginFailure struct {
	global.GVA_MODEL
	Scope     string    `json:"scope" gorm:"uniqueIndex:idx_scope_identity;size:16;not null;comment:platform|merchant|agent|member"`
	Identity  string    `json:"identity" gorm:"uniqueIndex:idx_scope_identity;size:128;not null;comment:账号标识"`
	FailCount int        `json:"failCount" gorm:"default:0;comment:连续失败次数"`
	LockedAt  *time.Time `json:"lockedAt" gorm:"comment:锁定时间"`
}

func (CsLoginFailure) TableName() string { return "cs_login_failures" }
