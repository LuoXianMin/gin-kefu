package response

import "time"

// ConversationRes 会话响应(用户侧不含客服真名, 仅工号)
type ConversationRes struct {
	ID             uint       `json:"id"`
	TenantID       uint       `json:"tenantId"`
	MemberID       uint       `json:"memberId"`
	MemberName     string     `json:"memberName"`     // 访客完整用户名(客服端列表/详情展示)
	MemberPhone    string     `json:"memberPhone"`    // 脱敏手机号(138****5678), 明文不出接口
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	CurrentAgentID uint       `json:"currentAgentId"`
	CurrentEmpNo   string     `json:"currentEmployeeNo"` // 用户侧展示 "客服008"
	LastEmpNo      string     `json:"lastEmployeeNo"`
	FirstAgentAt   *time.Time `json:"firstAgentAt"`
	IsFirstService bool       `json:"isFirstService"` // 无历史接待=首次接待
	Source         string     `json:"source"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// ConversationListItem 列表项
type ConversationListItem struct {
	ID             uint      `json:"id"`
	Status         string    `json:"status"`
	Title          string    `json:"title"`
	MemberID       uint      `json:"memberId"`
	CurrentAgentID uint      `json:"currentAgentId"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// EventRes 接待轨迹项
type EventRes struct {
	ID              uint      `json:"id"`
	ConversationID  uint      `json:"conversationId"`
	Action          string    `json:"action"`
	OperatorAgentID uint      `json:"operatorAgentId"`
	FromAgentID     uint      `json:"fromAgentId"`
	ToAgentID       uint      `json:"toAgentId"`
	FromEmpNo       string    `json:"fromEmployeeNo"`
	ToEmpNo         string    `json:"toEmployeeNo"`
	Reason          string    `json:"reason"`
	EventTime       time.Time `json:"eventTime"`
}

// MessageRes 消息响应
type MessageRes struct {
	MessageID      string    `json:"messageId"`
	ConversationID uint      `json:"conversationId"`
	Seq            int64     `json:"seq"`
	SenderType     string    `json:"senderType"` // member/agent
	SenderID       uint      `json:"senderId"`
	SenderEmpNo    string    `json:"senderEmployeeNo"` // 用户侧仅见工号
	Type           string    `json:"type"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
}

// SendMsgRes 发送 ACK
type SendMsgRes struct {
	MessageID    string    `json:"messageId"`
	Seq          int64     `json:"seq"`
	ServerTime   time.Time `json:"serverTime"`
	ClientMsgID  string    `json:"clientMsgId"`
	Deduplicated bool      `json:"deduplicated"` // 命中幂等, 返回原结果
}

// UserSidebarRes 客服工作台侧栏用户资料(最小权限, 电话脱敏可选)
type UserSidebarRes struct {
	MemberID  uint                  `json:"memberId"`
	Name      string                `json:"name"`
	Phone     string                `json:"phone"` // 按最小权限返回(脱敏)
	Source    string                `json:"source"`
	TenantID  uint                  `json:"tenantId"`
	CreatedAt time.Time             `json:"createdAt"`
	Summary   *MemberServiceSummary `json:"summary"` // 跨会话历史接待汇总
}

// MemberServiceSummary 用户维度历史接待汇总(只回工号不回真名)
type MemberServiceSummary struct {
	TotalConversations int64              `json:"totalConversations"`
	Servers            []ServerContactRes `json:"servers"`
}

type ServerContactRes struct {
	EmployeeNo string    `json:"employeeNo"`
	ServedAt   time.Time `json:"servedAt"` // 该客服首次接待时间
}

// HistoryRes 历史查询结果(含 7 天窗口标记)
type HistoryRes struct {
	Messages      []MessageRes `json:"messages"`
	WindowLimited bool         `json:"windowLimited"` // 用户侧 true: 仅最近7天
	MinRemainDays int          `json:"-"`
}
