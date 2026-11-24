package cs

import (
	"context"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"go.uber.org/zap"
)

// ConvEvent 会话生命周期事件(新建/接入/转接/结束), 经独立 Kafka topic 供 ws-gateway 实时推送。
// 与消息事件分离: 不落库(审计轨迹仍写 cs_assignment_events), 仅用于前端实时刷新。
type ConvEvent struct {
	TenantID        uint      `json:"tenantId"`
	ConversationID  uint      `json:"conversationId"`
	Op              string    `json:"op"` // created|claimed|transferred|closed
	Status          string    `json:"status"`
	MemberID        uint      `json:"memberId,omitempty"`
	AgentID         uint      `json:"agentId,omitempty"`
	AgentEmployeeNo string    `json:"agentEmployeeNo,omitempty"`
	TS              time.Time `json:"ts"`
}

// EmitConvEvent 尽力而为地投递会话事件: 事件通道失败不阻断会话操作(前端最坏退化为手动刷新)
func EmitConvEvent(ev *ConvEvent) {
	if ev == nil || !global.GVA_CONFIG.Kafka.Enabled {
		return
	}
	ev.TS = time.Now().UTC()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				global.GVA_LOG.Error("conv event panic", zap.Any("recover", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := produceConvEvent(ctx, ev); err != nil {
			global.GVA_LOG.Warn("conv event produce failed",
				zap.Uint("convID", ev.ConversationID), zap.String("op", ev.Op), zap.Error(err))
		}
	}()
}
