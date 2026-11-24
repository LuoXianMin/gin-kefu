package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"github.com/flipped-aurora/gin-vue-admin/server/core"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// storage-worker: 消费 Kafka(cg-storage) → 幂等落库 MongoDB → 推进 PG seq 水位。
// at-least-once: 仅对落库成功的记录 MarkRecords, 失败记录随下次 poll 重试(靠 message_id upsert 幂等)。
func main() {
	core.WorkerInit()
	log := global.GVA_LOG

	group := global.GVA_CONFIG.Kafka.StorageGroup
	if group == "" {
		group = "cg-storage"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cssvc.EnsureMessageIndexes(ctx); err != nil {
		log.Error("storage-worker ensure indexes failed", zap.Error(err))
	}

	cli, err := cssvc.NewConsumer(group)
	if err != nil {
		log.Fatal("storage-worker consumer init failed", zap.Error(err))
	}
	defer cli.Close()

	log.Info("storage-worker started", zap.String("group", group), zap.String("topic", global.GVA_CONFIG.Kafka.Topic))

	for {
		if ctx.Err() != nil {
			log.Info("storage-worker shutting down")
			return
		}
		fetches := cli.PollRecords(ctx, 200)
		if fetches.Empty() {
			continue
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				log.Warn("storage-worker poll error", zap.String("topic", e.Topic), zap.Error(e.Err))
			}
		}

		var ok []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			var ev cssvc.MessageEvent
			if err := json.Unmarshal(r.Value, &ev); err != nil {
				log.Error("storage-worker decode failed, dropping", zap.Error(err))
				ok = append(ok, r) // 脏数据直接提交丢弃, 避免堵塞分区
				return
			}
			inserted, err := cssvc.SaveMessage(ctx, &ev)
			if err != nil {
				log.Error("storage-worker save failed, will retry", zap.String("msgId", ev.MessageID), zap.Error(err))
				return // 不标记 → 下次重试
			}
			if err := cssvc.AdvanceSeqWatermark(ctx, ev.ConversationID, ev.Seq); err != nil {
				log.Warn("storage-worker advance watermark failed", zap.Uint("convID", ev.ConversationID), zap.Error(err))
			}
			ok = append(ok, r)
			if inserted {
				log.Debug("storage-worker persisted message", zap.String("msgId", ev.MessageID), zap.Int64("seq", ev.Seq))
			}
		})
		if len(ok) > 0 {
			cli.MarkCommitRecords(ok...)
		}
	}
}
