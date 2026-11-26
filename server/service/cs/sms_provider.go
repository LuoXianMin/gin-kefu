package cs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
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
	endpoint     string
	accessKey    string
	secretKey    string
	signName     string
	templateCode string
	client       *http.Client
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
	smsClientOnce    SmsSender
	smsClientOnceErr error
)

const (
	aliyunSMSEndpointDefault = "https://dysmsapi.aliyuncs.com"
	aliyunSMSVersion         = "2017-05-25"
	aliyunSMSRegionDefault   = "cn-hangzhou"
)

// AliyunSmsProvider 阿里云短信(Dysmsapi SendSms) 通道
// 采用官方 OpenAPI RPC 签名(V1/HMAC-SHA1), 不引入 SDK; 凭证只从环境变量读取, 任何情况下不得写日志
type AliyunSmsProvider struct {
	endpoint         string
	region           string
	accessKey        string
	secretKey        string
	signName         string
	templateCode     string
	templateParamKey string
	client           *http.Client
	nowFunc          func() time.Time // 测试可注入时间/随机数以固定签名
	nonceFunc        func() string
}

// percentEncode 按阿里云 OpenAPI 规则转义: 先标准 urlencode, 再把 '+' 还原为 %20, '*' 转 %2A, ~ 不转义
func percentEncode(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}

// aliyunSignature StringToSign 以 (AccessKeySecret + "&") 为密钥做 HMAC-SHA1 再 Base64
func aliyunSignature(secretKey, stringToSign string) string {
	mac := hmac.New(sha1.New, []byte(secretKey+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// signedQuery 将参数按 key 字节序排列拼成规范查询串, 再追加 Signature
func (p *AliyunSmsProvider) signedQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, percentEncode(k)+"="+percentEncode(params[k]))
	}
	canonical := strings.Join(parts, "&")
	stringToSign := http.MethodGet + "&" + percentEncode("/") + "&" + percentEncode(canonical)
	return canonical + "&Signature=" + percentEncode(aliyunSignature(p.secretKey, stringToSign))
}

func (p *AliyunSmsProvider) Send(ctx context.Context, phone, code string) error {
	if p.accessKey == "" || p.secretKey == "" {
		return errors.New("aliyun sms: 缺少 AccessKey 凭证")
	}
	if p.signName == "" || p.templateCode == "" {
		return errors.New("aliyun sms: 缺少签名或模板配置")
	}
	paramJSON, err := json.Marshal(map[string]string{p.templateParamKey: code})
	if err != nil {
		return err
	}
	now := p.nowFunc()
	reqParams := map[string]string{
		"AccessKeyId":      p.accessKey,
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     phone,
		"RegionId":         p.region,
		"SignName":         p.signName,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   p.nonceFunc(),
		"SignatureVersion": "1.0",
		"TemplateCode":     p.templateCode,
		"TemplateParam":    string(paramJSON),
		"Timestamp":        now.UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          aliyunSMSVersion,
	}
	reqURL := strings.TrimRight(p.endpoint, "/") + "/?" + p.signedQuery(reqParams)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("aliyun sms 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("aliyun sms 读取响应失败: %w", err)
	}
	var out struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		RequestId string `json:"RequestId"`
		BizId     string `json:"BizId"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("aliyun sms 响应解析失败(http %d)", resp.StatusCode)
	}
	if out.Code != "OK" {
		// 不透传原始报文以外敏感信息, 但保留 Code 便于区分模板/签名/频控等具体拒因
		return fmt.Errorf("aliyun sms 发送失败: %s %s(RequestId=%s)", out.Code, out.Message, out.RequestId)
	}
	global.GVA_LOG.Info("[sms-aliyun] 验证码已发送",
		zap.String("phone", maskPhone(phone)), zap.String("bizId", out.BizId))
	return nil
}

func newAliyunSmsProvider(cfg config.Sms) (SmsSender, error) {
	ak := os.Getenv(cfg.AccessKeyEnv)
	sk := os.Getenv(cfg.SecretKeyEnv)
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("aliyun sms: 环境变量 %s/%s 未配置", cfg.AccessKeyEnv, cfg.SecretKeyEnv)
	}
	if cfg.SignName == "" || cfg.TemplateCode == "" {
		return nil, errors.New("aliyun sms: sms.sign-name / sms.template-code 未配置")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = aliyunSMSEndpointDefault
	}
	region := cfg.Region
	if region == "" {
		region = aliyunSMSRegionDefault
	}
	paramKey := cfg.TemplateParamKey
	if paramKey == "" {
		paramKey = "code" // 阿里云模板里的变量名, 默认 ${code}
	}
	return &AliyunSmsProvider{
		endpoint:         endpoint,
		region:           region,
		accessKey:        ak,
		secretKey:        sk,
		signName:         cfg.SignName,
		templateCode:     cfg.TemplateCode,
		templateParamKey: paramKey,
		client:           &http.Client{Timeout: 10 * time.Second},
		nowFunc:          time.Now,
		nonceFunc:        newNonce,
	}, nil
}

// newNonce 阿里云 SignatureNonce 要求全局唯一, 用随机 hex 满足
func newNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(b)
}

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
	case "aliyun":
		sender, err := newAliyunSmsProvider(cfg)
		if err != nil {
			return nil, err
		}
		smsClientOnce = sender
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
