package cs

import (
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// CsAuditLog 安全/管理审计(追加式)。记录管理动作、越权尝试、租户状态变更等。
type CsAuditLog struct {
	global.GVA_MODEL
	TenantID  uint      `json:"tenantId" gorm:"index;comment:租户ID(平台动作为0)"`
	ActorType string    `json:"actorType" gorm:"size:16;not null;comment:platform/merchant/agent/member/system"`
	ActorID   uint      `json:"actorId" gorm:"index;comment:操作者账号ID"`
	Action    string    `json:"action" gorm:"size:64;not null;index;comment:动作码"`
	Target    string    `json:"target" gorm:"size:128;comment:目标, 如 tenant:7 / agent:3"`
	Detail    string    `json:"detail" gorm:"size:512;comment:备注/拒绝原因"`
	IP        string    `json:"ip" gorm:"size:64;comment:来源IP"`
	EventAt   time.Time `json:"eventAt" gorm:"not null;index;comment:UTC时间"`
}

func (CsAuditLog) TableName() string { return "cs_audit_logs" }
