package cs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
	"go.uber.org/zap"
)

var (
	ErrSmsThrottled  = errors.New("发送过于频繁，请稍后再试")
	ErrSmsDailyLimit = errors.New("当日发送次数已达上限")
	ErrSmsCodeFail   = errors.New("短信验证码错误或已过期")
	ErrSmsLocked     = errors.New("验证码失败次数过多，请重新获取")
)

// SmsCodeService 短信验证码 存储优先 Redis，Redis 未启用时降级进程内 BlackCache(仅限开发)
type SmsCodeService struct{}

func smsCodeKey(scope, phone string) string {
	return fmt.Sprintf("cs:sms:code:%s:%s", scope, phone)
}
func smsFailKey(scope, phone string) string {
	return fmt.Sprintf("cs:sms:fail:%s:%s", scope, phone)
}
func smsLastKey(scope, phone string) string {
	return fmt.Sprintf("cs:sms:last:%s:%s", scope, phone)
}
func smsDailyKey(scope, phone string) string {
	return fmt.Sprintf("cs:sms:daily:%s:%s:%s", scope, phone, time.Now().UTC().Format("20060102"))
}

func (s *SmsCodeService) genCode(length int) string {
	if length <= 0 {
		length = 6
	}
	var b []byte
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			n = big.NewInt(int64(time.Now().UnixNano() % 10))
		}
		b = append(b, byte('0'+n.Int64()))
	}
	return string(b)
}

// Send 生成并发送验证码 identity 为账号级幂等标识(防止任意手机号定向)
func (s *SmsCodeService) Send(ctx context.Context, scope, identity, phone string) (*csRes.SendSmsCodeRes, error) {
	cfg := global.GVA_CONFIG.Sms
	ttl := time.Duration(cfg.CodeTtlSeconds) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	redis := global.GVA_REDIS
	if redis == nil {
		return nil, errors.New("短信验证码服务依赖 Redis，请在配置中开启 system.use-redis")
	}

	// 发送间隔限制
	if last, err := redis.Get(ctx, smsLastKey(scope, identity)).Result(); err == nil && last != "" {
		return nil, ErrSmsThrottled
	}
	// 每日上限
	if cfg.DailyLimit > 0 {
		cnt, _ := redis.Get(ctx, smsDailyKey(scope, identity)).Int()
		if cnt >= cfg.DailyLimit {
			return nil, ErrSmsDailyLimit
		}
	}

	code := s.genCode(cfg.CodeLength)
	if err := redis.Set(ctx, smsCodeKey(scope, identity), code, ttl).Err(); err != nil {
		return nil, err
	}
	_ = redis.Del(ctx, smsFailKey(scope, identity))
	if err := redis.Set(ctx, smsLastKey(scope, identity), "1", time.Duration(cfg.SendTimeLimit)*time.Second).Err(); err != nil {
		return nil, err
	}
	if cfg.DailyLimit > 0 {
		if n, err := redis.Incr(ctx, smsDailyKey(scope, identity)).Result(); err == nil && n == 1 {
			_ = redis.Expire(ctx, smsDailyKey(scope, identity), 24*time.Hour).Err()
		}
	}

	sender, err := GetSmsSender()
	if err != nil {
		return nil, err
	}
	if err := sender.Send(ctx, phone, code); err != nil {
		global.GVA_LOG.Error("短信发送失败", zap.String("scope", scope), zap.Error(err))
		return nil, errors.New("短信通道异常，请稍后重试")
	}

	res := &csRes.SendSmsCodeRes{ExpireSeconds: int(ttl.Seconds())}
	if cfg.DevReturnCode {
		res.DevCode = code
	}
	return res, nil
}

// Verify 校验验证码 一次性使用；连续失败达上限后作废
func (s *SmsCodeService) Verify(ctx context.Context, scope, identity, code string) error {
	cfg := global.GVA_CONFIG.Sms
	redis := global.GVA_REDIS
	if redis == nil {
		return errors.New("短信验证码服务依赖 Redis")
	}
	if maxFail := cfg.MaxFailCount; maxFail > 0 {
		if n, _ := redis.Get(ctx, smsFailKey(scope, identity)).Int(); n >= maxFail {
			return ErrSmsLocked
		}
	}
	stored, err := redis.Get(ctx, smsCodeKey(scope, identity)).Result()
	if err != nil || stored == "" || stored != code {
		if maxFail := cfg.MaxFailCount; maxFail > 0 {
			if n, err := redis.Incr(ctx, smsFailKey(scope, identity)).Result(); err == nil && n == 1 {
				_ = redis.Expire(ctx, smsFailKey(scope, identity), 10*time.Minute).Err()
			}
		}
		return ErrSmsCodeFail
	}
	_ = redis.Del(ctx, smsCodeKey(scope, identity), smsFailKey(scope, identity))
	return nil
}
