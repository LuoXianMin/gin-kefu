package cs

import "time"

// MessageEvent 消息事件(Kafka 载荷 + MongoDB 文档)。时间为 UTC。
// 以 conversation_id 为 Kafka key 保证同会话分区内有序。
type MessageEvent struct {
	TenantID        uint      `json:"tenantId" bson:"tenant_id"`
	ConversationID  uint      `json:"conversationId" bson:"conversation_id"`
	MessageID       string    `json:"messageId" bson:"message_id"`
	Seq             int64     `json:"seq" bson:"seq"`
	SenderType      string    `json:"senderType" bson:"sender_type"` // member/agent
	SenderID        uint      `json:"senderId" bson:"sender_id"`
	Type            string    `json:"type" bson:"type"`
	Body            string    `json:"body" bson:"body"`
	ClientMsgID     string    `json:"clientMsgId" bson:"client_msg_id"`
	AssigneeAgentID uint      `json:"assigneeAgentId" bson:"assignee_agent_id"` // 发送时刻的当值客服(0=待接入), 租户通道定向提醒依据
	CreatedAt       time.Time `json:"createdAt" bson:"created_at"`
}
