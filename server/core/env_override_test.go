package core

import (
	"testing"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// TestApplyEnvOverridesSms 验证短信段 env 覆盖: 非空生效、未设置/非法值保持 yaml 原值
func TestApplyEnvOverridesSms(t *testing.T) {
	global.GVA_CONFIG.Sms.Provider = "log"
	global.GVA_CONFIG.Sms.TemplateCode = "SMS_YAML"
	global.GVA_CONFIG.Sms.DevReturnCode = true

	t.Setenv("GVA_SMS_PROVIDER", "http")
	t.Setenv("GVA_SMS_TEMPLATE_CODE", "SMS_ENV")
	t.Setenv("GVA_SMS_DEV_RETURN_CODE", "false")
	ApplyEnvOverrides()
	if global.GVA_CONFIG.Sms.Provider != "http" || global.GVA_CONFIG.Sms.TemplateCode != "SMS_ENV" || global.GVA_CONFIG.Sms.DevReturnCode {
		t.Fatalf("env 覆盖未生效: %+v", global.GVA_CONFIG.Sms)
	}

	// 清空后重新加载语义: 非法 bool 不应破坏现值
	global.GVA_CONFIG.Sms.DevReturnCode = true
	t.Setenv("GVA_SMS_DEV_RETURN_CODE", "not-a-bool")
	ApplyEnvOverrides()
	if !global.GVA_CONFIG.Sms.DevReturnCode {
		t.Fatal("非法 bool 值不应覆盖 yaml 原值")
	}
}

// TestApplyEnvOverridesSystem 验证环境类型/策略开关覆盖与其对后续分支的优先序
func TestApplyEnvOverridesSystem(t *testing.T) {
	global.GVA_CONFIG.System.Env = "local"
	global.GVA_CONFIG.System.OssType = "local"
	global.GVA_CONFIG.Sso.MaxAgeSec = 0
	global.GVA_CONFIG.Captcha.OpenCaptcha = 0
	global.GVA_CONFIG.Kafka.Brokers = []string{"gva-kafka:9092"}
	global.GVA_CONFIG.AliyunOSS.AccessKeyId = "yaml-ak"

	// oss-type 先被覆盖为 aliyun-oss, 随后 AK/SK 覆盖才能落到对应段
	t.Setenv("GVA_SYSTEM_ENV", "public")
	t.Setenv("GVA_OSS_TYPE", "aliyun-oss")
	t.Setenv("GVA_OSS_ACCESS_KEY_ID", "env-ak")
	t.Setenv("GVA_SSO_MAX_AGE_SEC", "300")
	t.Setenv("GVA_OPEN_CAPTCHA", "abc") // 非法值忽略
	t.Setenv("GVA_KAFKA_BROKERS", "k1:9092, k2:9092,")
	ApplyEnvOverrides()

	if global.GVA_CONFIG.System.Env != "public" {
		t.Fatal("GVA_SYSTEM_ENV 未覆盖 system.env")
	}
	if global.GVA_CONFIG.AliyunOSS.AccessKeyId != "env-ak" {
		t.Fatal("oss-type 应先于 AK/SK 覆盖生效")
	}
	if global.GVA_CONFIG.Sso.MaxAgeSec != 300 {
		t.Fatal("GVA_SSO_MAX_AGE_SEC 未覆盖")
	}
	if global.GVA_CONFIG.Captcha.OpenCaptcha != 0 {
		t.Fatal("非法 int 不应覆盖 yaml 原值")
	}
	if len(global.GVA_CONFIG.Kafka.Brokers) != 2 || global.GVA_CONFIG.Kafka.Brokers[1] != "k2:9092" {
		t.Fatalf("kafka brokers 解析错误: %v", global.GVA_CONFIG.Kafka.Brokers)
	}
}
