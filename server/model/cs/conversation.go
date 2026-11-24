package cs

import (
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// 会话状态
const (
	ConvStatusPending = "pending" // 待接入
	ConvStatusActive  = "active"  // 接待中
	ConvStatusClosed  = "closed"  // 已结束
)

// 接待轨迹动作类型
const (
	EventAssign   = "assign"   // 接入(抢单)
	EventTransfer = "transfer" // 转接
	EventClose    = "close"    // 结束
	EventReopen   = "reopen"   // 结束后再次咨询
)

// CsConversation 客服会话。当前接待客服为快照，权威轨迹见 events 表。
type CsConversation struct {
	global.GVA_MODEL
	TenantID      uint       `json:"tenantId" gorm:"index;not null;comment:租户ID"`
	MemberID      uint       `json:"memberId" gorm:"index;not null;comment:用户ID"`
	Title         string     `json:"title" gorm:"size:128;comment:会话标题"`
	Status        string     `json:"status" gorm:"size:16;default:pending;index;comment:pending/active/closed"`
	CurrentAgentID uint      `json:"currentAgentId" gorm:"index;comment:当前接待客服(0表示未分配)"`
	LastAgentID   uint       `json:"lastAgentId" gorm:"comment:上一接待客服"`
	LastAgentAt   *time.Time `json:"lastAgentAt" gorm:"comment:上一接待结束时间"`
	FirstAgentAt  *time.Time `json:"firstAgentAt" gorm:"comment:首次接待时间"`
	SeqWatermark  int64      `json:"seqWatermark" gorm:"default:0;comment:PG侧消息seq水位(异步维护)"`
	Source        string     `json:"source" gorm:"size:64;comment:来源渠道"`
	ClosedAt      *time.Time `json:"closedAt" gorm:"comment:结束时间"`
}

func (CsConversation) TableName() string { return "cs_conversations" }

// CsConversationMember 会话成员关系(用户与客服的可见范围), 用于 IDOR/BOLA 校验
type CsConversationMember struct {
	global.GVA_MODEL
	TenantID      uint `json:"tenantId" gorm:"index;not null"`
	ConversationID uint `json:"conversationId" gorm:"index;not null;uniqueIndex:idx_conv_member"`
	Scope         string `json:"scope" gorm:"size:16;comment:member/agent"`
	AccountID     uint   `json:"accountId" gorm:"not null;uniqueIndex:idx_conv_member"`
}

func (CsConversationMember) TableName() string { return "cs_conversation_members" }

// CsAssignmentEvent 接待轨迹(追加记录, 不覆盖)
type CsAssignmentEvent struct {
	global.GVA_MODEL
	TenantID       uint      `json:"tenantId" gorm:"index;not null"`
	ConversationID uint      `json:"conversationId" gorm:"index;not null"`
	OperatorAgentID uint     `json:"operatorAgentId" gorm:"comment:操作客服"`
	Action         string    `json:"action" gorm:"size:16;not null;comment:assign/transfer/close/reopen"`
	FromAgentID    uint      `json:"fromAgentId" gorm:"comment:转出客服"`
	ToAgentID      uint      `json:"toAgentId" gorm:"comment:转入客服"`
	Reason         string    `json:"reason" gorm:"size:256"`
	RequestID      string    `json:"requestId" gorm:"size:64;comment:请求关联ID"`
	EventTime      time.Time `json:"eventTime" gorm:"not null;comment:UTC事件时间"`
}

func (CsAssignmentEvent) TableName() string { return "cs_assignment_events" }
