package core

import (
	"os"
	"strconv"
	"strings"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// envOverride 环境变量非空时覆盖配置项(生产敏感信息集中到部署环境的 .env / Secret,
// 避免真实凭证落进 config yaml; 未设置则保持 yaml 原值, 完全向后兼容)
func envOverride(target *string, env string) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		*target = v
	}
}

// ApplyEnvOverrides 用环境变量覆盖配置中的敏感字段。
// server / storage-worker / ws-gateway 三个入口都经 Viper() 调用, 行为一致。
func ApplyEnvOverrides() {
	cfg := &global.GVA_CONFIG

	// 环境类型开关先行覆盖(后面的主库/OSS 覆盖按新类型选目标段)
	envOverride(&cfg.System.Env, "GVA_SYSTEM_ENV")                              // local|public; public=不回显图形验证码答案
	envOverride(&cfg.System.DbType, "GVA_DB_TYPE")                              // pgsql|mysql|...
	envOverride(&cfg.System.OssType, "GVA_OSS_TYPE")                            // local|aliyun-oss|...
	envOverride(&cfg.System.RouterPrefix, "GVA_ROUTER_PREFIX")                  // 改动需同步前端与 e2e 脚本
	envOverrideBool(&cfg.System.DisableAutoMigrate, "GVA_DISABLE_AUTO_MIGRATE") // 生产建议 true(手动迁移)
	envOverride(&cfg.Zap.Level, "GVA_ZAP_LEVEL")                                // debug|info|warn|error

	// JWT 签名密钥(所有令牌域与文件短链 HMAC 的根密钥)
	envOverride(&cfg.JWT.SigningKey, "GVA_JWT_SIGNING_KEY")

	// 主库连接凭据: 按 system.db-type 只覆盖实际选用的库
	db := mainDBGeneral()
	if db != nil {
		envOverride(&db.Username, "GVA_DB_USERNAME")
		envOverride(&db.Password, "GVA_DB_PASSWORD")
		envOverride(&db.Dbname, "GVA_DB_NAME")
	}

	// Redis / MongoDB 凭据
	envOverride(&cfg.Redis.Password, "GVA_REDIS_PASSWORD")
	envOverride(&cfg.Mongo.Username, "GVA_MONGO_USERNAME")
	envOverride(&cfg.Mongo.Password, "GVA_MONGO_PASSWORD")

	// 短信通道环境依赖配置(provider/网关/签名/模板等随部署环境不同);
	// AK/SK 由 sms.access-key-env 指定的变量原生读取, 不在此覆盖
	envOverride(&cfg.Sms.Provider, "GVA_SMS_PROVIDER")                   // log | http | aliyun
	envOverride(&cfg.Sms.Endpoint, "GVA_SMS_ENDPOINT")                   // http 模式短信网关地址; aliyun 可留空
	envOverride(&cfg.Sms.SignName, "GVA_SMS_SIGN_NAME")                  // 短信签名
	envOverride(&cfg.Sms.TemplateCode, "GVA_SMS_TEMPLATE_CODE")          // 短信模板 Code
	envOverride(&cfg.Sms.Region, "GVA_SMS_REGION")                       // aliyun 地域, 留空=cn-hangzhou
	envOverride(&cfg.Sms.TemplateParamKey, "GVA_SMS_TEMPLATE_PARAM_KEY") // aliyun 模板验证码变量名, 留空=code
	envOverrideBool(&cfg.Sms.DevReturnCode, "GVA_SMS_DEV_RETURN_CODE")   // 生产必须 false(验证码回显=MFA 旁路)

	// SSO 与图形验证码策略
	envOverrideInt(&cfg.Sso.MaxAgeSec, "GVA_SSO_MAX_AGE_SEC")    // >0 时校验 SSO 链接 ts 时效(秒)
	envOverrideInt(&cfg.Captcha.OpenCaptcha, "GVA_OPEN_CAPTCHA") // 0=每次登录都要图形码, >0=错 N 次后出码

	// Kafka 消息链路(生产换集群地址; 逗号分隔列表)
	envOverrideBool(&cfg.Kafka.Enabled, "GVA_KAFKA_ENABLED")
	if v := strings.TrimSpace(os.Getenv("GVA_KAFKA_BROKERS")); v != "" {
		brokers := make([]string, 0, 8)
		for _, b := range strings.Split(v, ",") {
			if b = strings.TrimSpace(b); b != "" {
				brokers = append(brokers, b)
			}
		}
		if len(brokers) > 0 {
			cfg.Kafka.Brokers = brokers
		}
	}

	// OSS 凭据: 按 system.oss-type 覆盖对应段(local 无凭据, 自然跳过)
	ak, sk := os.Getenv("GVA_OSS_ACCESS_KEY_ID"), os.Getenv("GVA_OSS_ACCESS_KEY_SECRET")
	switch strings.ToLower(cfg.System.OssType) {
	case "aliyun-oss":
		overridePair(&cfg.AliyunOSS.AccessKeyId, &cfg.AliyunOSS.AccessKeySecret, ak, sk)
	case "minio":
		overridePair(&cfg.Minio.AccessKeyId, &cfg.Minio.AccessKeySecret, ak, sk)
	case "qiniu":
		overridePair(&cfg.Qiniu.AccessKey, &cfg.Qiniu.SecretKey, ak, sk)
	case "tencent-cos":
		overridePair(&cfg.TencentCOS.SecretID, &cfg.TencentCOS.SecretKey, ak, sk)
	case "aws-s3":
		overridePair(&cfg.AwsS3.SecretID, &cfg.AwsS3.SecretKey, ak, sk)
	case "cloudflare-r2":
		overridePair(&cfg.CloudflareR2.AccessKeyID, &cfg.CloudflareR2.SecretAccessKey, ak, sk)
	case "hua-wei-obs":
		overridePair(&cfg.HuaWeiObs.AccessKey, &cfg.HuaWeiObs.SecretKey, ak, sk)
	}
}

// envOverrideBool 环境变量非空且可解析为 bool 时覆盖(非法值忽略, 保持 yaml 原值)
func envOverrideBool(target *bool, env string) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*target = b
		}
	}
}

// envOverrideInt 环境变量非空且可解析为 int 时覆盖(非法值忽略, 保持 yaml 原值)
func envOverrideInt(target *int, env string) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*target = n
		}
	}
}

// mainDBGeneral 返回当前 db-type 对应主库配置的 GeneralDB 指针
func mainDBGeneral() *config.GeneralDB {
	cfg := &global.GVA_CONFIG
	switch strings.ToLower(cfg.System.DbType) {
	case "mysql":
		return &cfg.Mysql.GeneralDB
	case "pgsql":
		return &cfg.Pgsql.GeneralDB
	case "mssql":
		return &cfg.Mssql.GeneralDB
	case "oracle":
		return &cfg.Oracle.GeneralDB
	case "sqlite":
		return &cfg.Sqlite.GeneralDB
	default:
		return nil
	}
}

func overridePair(id, secret *string, ak, sk string) {
	if strings.TrimSpace(ak) != "" {
		*id = strings.TrimSpace(ak)
	}
	if strings.TrimSpace(sk) != "" {
		*secret = strings.TrimSpace(sk)
	}
}
