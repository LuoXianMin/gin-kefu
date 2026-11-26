package cs

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// liveSmsConfig 单测进程不会加载 yaml, 因此缺项时直接从环境变量自举(与部署侧 .env 同名)
func liveSmsConfig() config.Sms {
	cfg := global.GVA_CONFIG.Sms
	if cfg.Provider == "" {
		cfg.Provider = "aliyun"
	}
	if cfg.AccessKeyEnv == "" {
		cfg.AccessKeyEnv = "GVA_SMS_AK"
	}
	if cfg.SecretKeyEnv == "" {
		cfg.SecretKeyEnv = "GVA_SMS_SK"
	}
	if cfg.SignName == "" {
		cfg.SignName = os.Getenv("GVA_SMS_SIGN_NAME")
	}
	if cfg.TemplateCode == "" {
		cfg.TemplateCode = os.Getenv("GVA_SMS_TEMPLATE_CODE")
	}
	if cfg.TemplateParamKey == "" {
		cfg.TemplateParamKey = os.Getenv("GVA_SMS_TEMPLATE_PARAM_KEY")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = os.Getenv("GVA_SMS_ENDPOINT")
	}
	return cfg
}

// TestAliyunLiveSignatureAccepted 真实联通性校验(默认跳过):
//
//	GVA_SMS_LIVE=1 go test ./service/cs/ -run TestAliyunLiveSignatureAccepted -v
//
// 故意使用非法手机号, 阿里云会在"发送"前置校验阶段拒绝, 因此不产生扣费也不会打扰任何号码;
// 但鉴权与签名已在此之前完成, 由返回 Code 即可判定我们的签名实现是否被接受。
func TestAliyunLiveSignatureAccepted(t *testing.T) {
	if os.Getenv("GVA_SMS_LIVE") != "1" {
		t.Skip("设置 GVA_SMS_LIVE=1 并配好真实凭证后才执行")
	}
	cfg := liveSmsConfig()
	if cfg.Provider != "aliyun" {
		t.Skipf("sms.provider=%q, 非 aliyun, 跳过", cfg.Provider)
	}
	sender, err := newAliyunSmsProvider(cfg)
	if err != nil {
		t.Fatalf("构建通道失败: %v", err)
	}

	err = sender.Send(context.Background(), "00000000000", "123456")
	if err == nil {
		t.Skip("阿里云接受了该手机号, 无法据此判断签名结论, 请检查用例")
	}
	msg := err.Error()
	t.Logf("阿里云返回: %s", msg)

	// 签名/凭证类错误说明我们的实现不被接受; 其余业务错误(手机号非法等)反证鉴权已通过
	authFailures := []string{
		"SignatureDoesNotMatch", "isv.SMS_SIGNATURE_ILLEGAL", "InvalidAccessKeyId",
		"IncompleteSignature", "MissingAccessKeyId", "Forbidden.AccessKeyDisabled",
		"InvalidAccessKeySecret", "SignatureNonceUsed",
	}
	for _, bad := range authFailures {
		if strings.Contains(msg, bad) {
			t.Fatalf("签名/凭证未被阿里云接受: %s -> %s", bad, msg)
		}
	}
	if !strings.Contains(msg, "isv.MOBILE_NUMBER_ILLEGAL") && !strings.Contains(msg, "INVALID_ACCOUNT") {
		t.Logf("非手机号非法类错误, 但鉴权已通过(无签名类报错), 视为签名实现正确: %s", msg)
	}
}

// TestAliyunLiveTemplateProbe 用真实号码验证模板变量名是否匹配(会产生一条短信, 需显式给号)
//
//	GVA_SMS_LIVE=1 GVA_SMS_LIVE_PHONE=13800138000 go test ./service/cs/ -run TestAliyunLiveTemplateProbe -v
func TestAliyunLiveTemplateProbe(t *testing.T) {
	phone := os.Getenv("GVA_SMS_LIVE_PHONE")
	if os.Getenv("GVA_SMS_LIVE") != "1" || phone == "" {
		t.Skip("需 GVA_SMS_LIVE=1 且 GVA_SMS_LIVE_PHONE=<自己手机号>(会真发一条短信)")
	}
	cfg := liveSmsConfig()
	if cfg.Provider != "aliyun" {
		t.Skipf("sms.provider=%q, 非 aliyun, 跳过", cfg.Provider)
	}
	sender, err := newAliyunSmsProvider(cfg)
	if err != nil {
		t.Fatalf("构建通道失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sender.Send(ctx, phone, "824137"); err != nil {
		t.Fatalf("真实发送失败(检查模板变量名是否为 %q): %v", cfg.TemplateParamKey, err)
	}
	t.Logf("已向 %s 发出验证码短信", maskPhone(phone))
}
