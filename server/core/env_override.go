package core

import (
	"os"
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
