package cs

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	"go.uber.org/zap"
)

// Audit 写审计日志(best-effort, 失败只记错误日志不影响主流程)
func Audit(tenantID uint, actorType string, actorID uint, action, target, detail, ip string) {
	if global.GVA_DB == nil {
		return
	}
	rec := cs.CsAuditLog{
		TenantID: tenantID, ActorType: actorType, ActorID: actorID,
		Action: action, Target: target, Detail: detail, IP: ip,
		EventAt: time.Now().UTC(),
	}
	if err := global.GVA_DB.Create(&rec).Error; err != nil {
		global.GVA_LOG.Error("audit log write failed", zap.String("action", action), zap.Error(err))
	}
}

// ListAudits 审计分页查询: tenantID>0 限本租户(商户侧), tenantID=0 全域(平台侧); action 支持精确码或前缀码(如 file / tenant.create)
func ListAudits(tenantID uint, action string, page, pageSize int) ([]cs.CsAuditLog, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	db := global.GVA_DB.Model(&cs.CsAuditLog{})
	if tenantID > 0 {
		db = db.Where("tenant_id = ?", tenantID)
	}
	if action != "" {
		db = db.Where("action = ? OR action LIKE ?", action, action+".%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []cs.CsAuditLog
	err := db.Order("event_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// 指标名(REDIS 计数器, /metrics 以 Prometheus 文本格式暴露)
const (
	MetricMsgSent      = "cs_msg_sent_total"           // 消息成功进入 Kafka
	MetricMsgRejected  = "cs_msg_rejected_total"       // 消息发送被拒(授权/容量/通道)
	MetricIdemHit      = "cs_msg_idempotent_hit_total" // 幂等命中
	MetricConvClaimed  = "cs_conv_claimed_total"       // 抢单成功
	MetricClaimDenied  = "cs_conv_claim_denied_total"  // 抢单失败(已被接/容量)
	MetricTransferDone = "cs_conv_transfer_total"      // 转接成功
	MetricLoginFailed  = "cs_login_failed_total"       // 登录失败
	MetricRateLimited  = "cs_rate_limited_total"       // 限流命中
	MetricFileUploaded = "cs_file_uploaded_total"      // 附件上传
	MetricFileInfected = "cs_file_infected_total"      // 附件扫描违规
)

var metricHelp = map[string]string{
	MetricMsgSent:      "Messages successfully produced to Kafka",
	MetricMsgRejected:  "Message send requests rejected",
	MetricIdemHit:      "Idempotent duplicate send hits",
	MetricConvClaimed:  "Conversation claim successes",
	MetricClaimDenied:  "Conversation claim denials",
	MetricTransferDone: "Conversation transfers completed",
	MetricLoginFailed:  "Failed login attempts",
	MetricRateLimited:  "Requests rejected by rate limiter",
	MetricFileUploaded: "Attachments uploaded",
	MetricFileInfected: "Attachments failed virus scan",
}

// metricKey Redis 键: 计数器按标签(label)细分
func metricKey(name, label string) string {
	if label == "" {
		return "cs:metric:" + name
	}
	return "cs:metric:" + name + ":" + label
}

// MetricIncr 计数 +1(Redis 不可用时静默跳过, 指标非关键路径)
func MetricIncr(ctx context.Context, name, label string) {
	if global.GVA_REDIS == nil {
		return
	}
	if err := global.GVA_REDIS.Incr(ctx, metricKey(name, label)).Err(); err != nil {
		global.GVA_LOG.Debug("metric incr failed", zap.String("metric", name), zap.Error(err))
	}
}

// SumMetric 聚合某计数器 base+全部 label 的总值(Redis 不可用时返 0)
func SumMetric(ctx context.Context, name string) int64 {
	if global.GVA_REDIS == nil {
		return 0
	}
	var total int64
	iter := global.GVA_REDIS.Scan(ctx, 0, "cs:metric:"+name+"*", 200).Iterator()
	for iter.Next(ctx) {
		v, err := global.GVA_REDIS.Get(ctx, iter.Val()).Int64()
		if err == nil {
			total += v
		}
	}
	return total
}

// RenderMetrics Prometheus 文本格式暴露(供单机采集; 生产接官方 client_golang + PushGateway)
func RenderMetrics(ctx context.Context) string {
	var sb strings.Builder
	// gauge: 会话状态分布
	gauges := []struct {
		key, help, query string
	}{
		{"cs_conv_pending", "Conversations waiting for pickup", "pending"},
		{"cs_conv_active", "Conversations in service", "active"},
		{"cs_conv_closed", "Closed conversations", "closed"},
	}
	for _, g := range gauges {
		var n int64
		if global.GVA_DB != nil {
			global.GVA_DB.Model(&cs.CsConversation{}).Where("status = ?", g.query).Count(&n)
		}
		fmt.Fprintf(&sb, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", g.key, g.help, g.key, g.key, n)
	}

	names := make([]string, 0, len(metricHelp))
	for n := range metricHelp {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&sb, "# HELP %s %s\n# TYPE %s counter\n", name, metricHelp[name], name)
		if global.GVA_REDIS == nil {
			fmt.Fprintf(&sb, "%s 0\n", name)
			continue
		}
		// 聚合 base + 各 label: 直接扫前缀
		iter := global.GVA_REDIS.Scan(ctx, 0, "cs:metric:"+name+":*", 100).Iterator()
		iterBase := global.GVA_REDIS.Scan(ctx, 0, "cs:metric:"+name, 100).Iterator()
		keys := make([]string, 0, 8)
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		for iterBase.Next(ctx) {
			keys = append(keys, iterBase.Val())
		}
		any := false
		for _, key := range keys {
			v, err := global.GVA_REDIS.Get(ctx, key).Int64()
			if err != nil {
				continue
			}
			label := strings.TrimPrefix(key, "cs:metric:"+name)
			label = strings.TrimPrefix(label, ":")
			any = true
			if label == "" {
				fmt.Fprintf(&sb, "%s %d\n", name, v)
			} else {
				fmt.Fprintf(&sb, "%s{label=%q} %d\n", name, label, v)
			}
		}
		if !any {
			fmt.Fprintf(&sb, "%s 0\n", name)
		}
	}
	return sb.String()
}
