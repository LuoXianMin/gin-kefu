package cs

import (
	"context"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
)

// AdvanceSeqWatermark 落库成功后把会话 seq 水位推进到已持久化的最大 seq(只前进不后退)。
// 用于 Redis seq 分配器丢失后的冷启动基线恢复(见 allocSeq)。best-effort: 失败不影响消息已入 Mongo 的事实。
func AdvanceSeqWatermark(ctx context.Context, convID uint, seq int64) error {
	return global.GVA_DB.WithContext(ctx).Model(&cs.CsConversation{}).
		Where("id = ? AND seq_watermark < ?", convID, seq).
		UpdateColumn("seq_watermark", seq).Error
}
