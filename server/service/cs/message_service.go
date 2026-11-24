package cs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageService 消息热路径: 授权校验 → 幂等 → seq 分配 → Kafka ACK
type MessageService struct{}

var (
	ErrConvClosed     = errors.New("会话已结束, 不能发送消息")
	ErrSeqUnavailable = errors.New("消息服务暂不可用, 请重试(序号分配器未就绪)")
	ErrIdemInProgress = errors.New("相同请求正在处理中, 请使用原幂等键重试")
	ErrMsgTypeInvalid = errors.New("不支持的消息类型(仅 text/image)")
	ErrMsgBodyInvalid = errors.New("消息体格式不合法")
	ErrMsgFileInvalid = errors.New("图片消息引用的文件不合法(不存在/未扫描通过/不属于本会话或非本人上传)")
	memberWindowDays  = 7
)

// imageBody image 类型消息的 body 约定结构

type imageBody struct {
	FileID   uint   `json:"fileId"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
}

// validateMsg 消息类型白名单与 image 附件校验(在幂等登记前拒绝脏请求)
func (s *MessageService) validateMsg(tenantID uint, senderType string, senderID, convID uint, req csReq.SendMsgReq) error {
	switch req.Type {
	case "text":
		if len(req.Body) == 0 || len(req.Body) > 8192 {
			return ErrMsgBodyInvalid
		}
		return nil
	case "image":
		var b imageBody
		if err := json.Unmarshal([]byte(req.Body), &b); err != nil || b.FileID == 0 {
			return ErrMsgBodyInvalid
		}
		var file cs.CsFileAttachment
		if err := global.GVA_DB.Where("id = ? AND tenant_id = ? AND conversation_id = ?", b.FileID, tenantID, convID).First(&file).Error; err != nil {
			return ErrMsgFileInvalid
		}
		// 扫描未通过不可发; 上传者必须是当前发送者, 防止借他人附件发消息
		if file.ScanStatus != cs.FileScanClean || file.UploaderType != senderType || file.UploaderID != senderID {
			return ErrMsgFileInvalid
		}
		return nil
	default:
		return ErrMsgTypeInvalid
	}
}

// CsReadCursor 已读水位(PG 强一致)
type CsReadCursor struct {
	global.GVA_MODEL
	TenantID       uint   `json:"tenantId" gorm:"index;not null"`
	ConversationID uint   `json:"conversationId" gorm:"not null;uniqueIndex:idx_cursor"`
	Scope          string `json:"scope" gorm:"size:16;not null;uniqueIndex:idx_cursor"`
	AccountID      uint   `json:"accountId" gorm:"not null;uniqueIndex:idx_cursor"`
	LastReadSeq    int64  `json:"lastReadSeq" gorm:"default:0"`
}

func (CsReadCursor) TableName() string { return "cs_read_cursors" }

func seqKey(convID uint) string { return fmt.Sprintf("cs:seq:%d", convID) }
func idemKey(tenantID uint, senderType string, senderID uint, clientMsgID string) string {
	return fmt.Sprintf("cs:idem:%d:%s:%d:%s", tenantID, senderType, senderID, clientMsgID)
}

const seqInitLua = `
local cur = redis.call('GET', KEYS[1])
if not cur then
  redis.call('SET', KEYS[1], ARGV[1])
end
return redis.call('INCR', KEYS[1])
`

// allocSeq 会话内单调递增 seq。冷启动时用 max(Mongo已落库最大seq, PG水位) 作为基线原子初始化。
// 注意: Redis 故障时直接拒绝发送(方案 §10.2), 不做跳过幂等的降级。
func (s *MessageService) allocSeq(ctx context.Context, conv *cs.CsConversation) (int64, error) {
	rdb := global.GVA_REDIS
	if rdb == nil {
		return 0, ErrSeqUnavailable
	}
	baseline := conv.SeqWatermark
	if mongoMax, err := MaxStoredSeq(ctx, conv.ID); err == nil && mongoMax > baseline {
		baseline = mongoMax
	}
	seq, err := rdb.Eval(ctx, seqInitLua, []string{seqKey(conv.ID)}, baseline).Int64()
	if err != nil {
		return 0, ErrSeqUnavailable
	}
	return seq, nil
}

type idemResult struct {
	Status    string `json:"status"` // pending|done
	MessageID string `json:"messageId"`
	Seq       int64  `json:"seq"`
}

// Send 发送消息热路径(仅授权后的可信调用)。成功语义=Kafka ISR 已确认。
func (s *MessageService) Send(ctx context.Context, tenantID uint, senderType string, senderID, convID uint, req csReq.SendMsgReq) (*csRes.SendMsgRes, error) {
	// 1) 授权: 会话存在 + 同租户 + 发送者是成员 + 状态可发送(不信任客户端会话ID)
	conv, allowed := (&ConversationService{}).CanAccess(tenantID, senderType, senderID, convID)
	if !allowed {
		MetricIncr(ctx, MetricMsgRejected, "denied")
		return nil, ErrConvNotFound
	}
	if conv.Status == cs.ConvStatusClosed {
		MetricIncr(ctx, MetricMsgRejected, "closed")
		return nil, ErrConvClosed
	}
	// 1.5) 类型白名单 + 附件合法性(拒绝在进入幂等/seq 分配之前)
	if err := s.validateMsg(tenantID, senderType, senderID, convID, req); err != nil {
		MetricIncr(ctx, MetricMsgRejected, "type")
		return nil, err
	}

	// 2) 幂等原子登记
	rdb := global.GVA_REDIS
	if rdb == nil {
		return nil, ErrSeqUnavailable
	}
	iKey := idemKey(tenantID, senderType, senderID, req.ClientMsgID)
	ok, err := rdb.SetNX(ctx, iKey, `{"status":"pending"}`, 24*time.Hour).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		raw, _ := rdb.Get(ctx, iKey).Result()
		var prev idemResult
		if json.Unmarshal([]byte(raw), &prev) == nil && prev.Status == "done" {
			MetricIncr(ctx, MetricIdemHit, senderType)
			return &csRes.SendMsgRes{MessageID: prev.MessageID, Seq: prev.Seq, ClientMsgID: req.ClientMsgID, Deduplicated: true}, nil
		}
		return nil, ErrIdemInProgress
	}

	// 3) 分配 seq + 生成 message_id
	seq, err := s.allocSeq(ctx, conv)
	if err != nil {
		rdb.Del(ctx, iKey) // 未产生消息, 释放幂等键允许重试
		return nil, err
	}
	ev := &MessageEvent{
		TenantID: tenantID, ConversationID: conv.ID, MessageID: uuid.NewString(),
		Seq: seq, SenderType: senderType, SenderID: senderID,
		Type: req.Type, Body: req.Body, ClientMsgID: req.ClientMsgID,
		AssigneeAgentID: conv.CurrentAgentID, // 服务端取当值客服, 网关据此定向未读提醒
		CreatedAt:       time.Now().UTC(),
	}

	// 4) 同步写 Kafka(acks=all), 成功才 ACK
	val, _ := json.Marshal(ev)
	if err := produceMessage(ctx, fmt.Sprintf("%d", conv.ID), val); err != nil {
		global.GVA_LOG.Error("kafka produce failed", zap.Uint("convID", conv.ID), zap.Error(err))
		MetricIncr(ctx, MetricMsgRejected, "kafka")
		rdb.Del(ctx, iKey) // Produce 失败: 释放幂等键, 客户端用同一 clientMsgId 重试
		return nil, fmt.Errorf("消息通道暂不可用, 请用原 clientMsgId 重试: %v", err)
	}
	MetricIncr(ctx, MetricMsgSent, senderType)

	// 5) 落幂等结果(已完成)
	done, _ := json.Marshal(idemResult{Status: "done", MessageID: ev.MessageID, Seq: ev.Seq})
	_ = rdb.Set(ctx, iKey, done, 24*time.Hour).Err()

	return &csRes.SendMsgRes{
		MessageID: ev.MessageID, Seq: ev.Seq, ServerTime: ev.CreatedAt,
		ClientMsgID: ev.ClientMsgID,
	}, nil
}

// SendWelcome 机器人欢迎语: 仅供服务端新会话创建后内部调用(非客户端通道, 跳过 CanAccess)。
// 幂等键固定 welcome-<convID>, 同一会话重复调用不会重发; 失败由调用方记日志不阻断建会话。
func (s *MessageService) SendWelcome(ctx context.Context, tenantID uint, conv *cs.CsConversation, text string) (*csRes.SendMsgRes, error) {
	if text == "" || conv == nil || conv.Status == cs.ConvStatusClosed {
		return nil, nil
	}
	rdb := global.GVA_REDIS
	if rdb == nil {
		return nil, ErrSeqUnavailable
	}
	iKey := idemKey(tenantID, cs.SenderBot, 0, fmt.Sprintf("welcome-%d", conv.ID))
	ok, err := rdb.SetNX(ctx, iKey, `{"status":"pending"}`, 7*24*time.Hour).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil // 已发过/发送中, 静默跳过
	}
	seq, err := s.allocSeq(ctx, conv)
	if err != nil {
		rdb.Del(ctx, iKey)
		return nil, err
	}
	ev := &MessageEvent{
		TenantID: tenantID, ConversationID: conv.ID, MessageID: uuid.NewString(),
		Seq: seq, SenderType: cs.SenderBot, SenderID: 0,
		Type: "text", Body: text, ClientMsgID: fmt.Sprintf("welcome-%d", conv.ID),
		CreatedAt: time.Now().UTC(),
	}
	val, _ := json.Marshal(ev)
	if err := produceMessage(ctx, fmt.Sprintf("%d", conv.ID), val); err != nil {
		rdb.Del(ctx, iKey)
		return nil, err
	}
	MetricIncr(ctx, MetricMsgSent, cs.SenderBot)
	done, _ := json.Marshal(idemResult{Status: "done", MessageID: ev.MessageID, Seq: ev.Seq})
	_ = rdb.Set(ctx, iKey, done, 7*24*time.Hour).Err()
	return &csRes.SendMsgRes{MessageID: ev.MessageID, Seq: ev.Seq, ServerTime: ev.CreatedAt, ClientMsgID: ev.ClientMsgID}, nil
}

// AgentHistory 客服侧全量历史(不受7天限制), 需先经 CanAccess 校验
func (s *MessageService) AgentHistory(ctx context.Context, tenantID, convID uint, q csReq.HistoryQueryReq) (*csRes.HistoryRes, error) {
	list, err := QueryMessages(ctx, convID, q.BeforeSeq, q.AfterSeq, q.Limit, time.Time{})
	if err != nil {
		return nil, err
	}
	return &csRes.HistoryRes{Messages: toMsgResList(list), WindowLimited: false}, nil
}

// MemberHistory 用户侧最近7天窗口(服务端按每条消息 created_at 过滤, 客户端参数不能扩大)
func (s *MessageService) MemberHistory(ctx context.Context, tenantID, convID uint, q csReq.HistoryQueryReq) (*csRes.HistoryRes, error) {
	since := time.Now().UTC().AddDate(0, 0, -memberWindowDays)
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50 // 用户侧强制上限
	}
	list, err := QueryMessages(ctx, convID, q.BeforeSeq, q.AfterSeq, q.Limit, since)
	if err != nil {
		return nil, err
	}
	return &csRes.HistoryRes{Messages: toMsgResList(list), WindowLimited: true}, nil
}

// toMsgResList 转换并填充客服工号(用户侧只见工号)
func toMsgResList(list []MessageEvent) []csRes.MessageRes {
	out := make([]csRes.MessageRes, 0, len(list))
	for i := range list {
		m := &list[i]
		res := csRes.MessageRes{
			MessageID: m.MessageID, ConversationID: m.ConversationID, Seq: m.Seq,
			SenderType: m.SenderType, SenderID: m.SenderID, Type: m.Type,
			Body: m.Body, CreatedAt: m.CreatedAt,
		}
		if m.SenderType == cs.ScopeAgent {
			res.SenderEmpNo = empNoOf(m.SenderID)
		}
		out = append(out, res)
	}
	return out
}

// ReportRead 上报已读水位(只前进不后退)
func (s *MessageService) ReportRead(tenantID, convID uint, scope string, accountID uint, lastReadSeq int64) error {
	return global.GVA_DB.Transaction(func(tx *gorm.DB) error {
		var cur CsReadCursor
		err := tx.Where("tenant_id = ? AND conversation_id = ? AND scope = ? AND account_id = ?",
			tenantID, convID, scope, accountID).First(&cur).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&CsReadCursor{TenantID: tenantID, ConversationID: convID, Scope: scope, AccountID: accountID, LastReadSeq: lastReadSeq}).Error
		}
		if err != nil {
			return err
		}
		if lastReadSeq <= cur.LastReadSeq {
			return nil
		}
		return tx.Model(&cur).Update("last_read_seq", lastReadSeq).Error
	})
}
