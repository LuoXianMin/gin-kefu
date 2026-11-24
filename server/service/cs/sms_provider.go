package cs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"go.uber.org/zap"
)

// SmsSender 短信通道抽象
type SmsSender interface {
	Send(ctx context.Context, phone, code string) error
}

// LogProvider 开发环境通道: 验证码写日志，不产生任何真实发送
type LogProvider struct{}

func (p *LogProvider) Send(ctx context.Context, phone, code string) error {
	global.GVA_LOG.Info("[sms-log] 开发环境验证码(未真实发送)",
		zap.String("phone", maskPhone(phone)), zap.String("code", code))
	return nil
}

// HttpProvider 通用 HTTP 网关通道 凭证从环境变量注入
type HttpProvider struct {
	endpoint      string
	accessKey     string
	secretKey     string
	signName      string
	templateCode  string
	client        *http.Client
}

func (p *HttpProvider) Send(ctx context.Context, phone, code string) error {
	body, _ := json.Marshal(map[string]string{
		"phone": phone,
		"code":  code,
		"sign":  p.signName,
		"tpl":   p.templateCode,
	})
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(p.secretKey))
	mac.Write(append(body, []byte(ts)...))
	signature := hex.EncodeToString(mac.Sum(nil)) // 示意: 实际签名算法按供应商SDK调整，绝不外泄原始密钥
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Access-Key", p.accessKey)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", signature)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("sms gateway responded " + resp.Status)
	}
	return nil
}

var (
	smsClientOnce SmsSender
	smsClientOnceErr error
)

// GetSmsSender 按配置构建通道(单例)
func GetSmsSender() (SmsSender, error) {
	if smsClientOnce != nil {
		return smsClientOnce, nil
	}
	cfg := global.GVA_CONFIG.Sms
	switch cfg.Provider {
	case "http":
		ak := os.Getenv(cfg.AccessKeyEnv)
		sk := os.Getenv(cfg.SecretKeyEnv)
		if cfg.Endpoint == "" || ak == "" || sk == "" {
			return nil, errors.New("sms http provider 配置不完整(endpoint/access-key-env/secret-key-env)")
		}
		smsClientOnce = &HttpProvider{
			endpoint:     cfg.Endpoint,
			accessKey:    ak,
			secretKey:    sk,
			signName:     cfg.SignName,
			templateCode: cfg.TemplateCode,
			client:       &http.Client{Timeout: 5 * time.Second},
		}
	case "log":
		smsClientOnce = &LogProvider{}
	default:
		return nil, fmt.Errorf("未知短信通道 provider: %q", cfg.Provider)
	}
	return smsClientOnce, smsClientOnceErr
}

// maskPhone 日志脱敏
func maskPhone(phone string) string {
	if len(phone) < 7 {
		return "****"
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}
