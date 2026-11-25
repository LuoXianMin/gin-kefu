package config

// Sso 商户渠道免密进入配置(h5 ?user= AES 加密参数)
// 生产密钥通过环境变量注入, 不落仓库/镜像
type Sso struct {
	AesKeyEnv string `mapstructure:"aes-key-env" json:"aes-key-env" yaml:"aes-key-env"` // 读取 AES 密钥的环境变量名(空则禁用 SSO 接口)
	MaxAgeSec int    `mapstructure:"max-age-sec" json:"max-age-sec" yaml:"max-age-sec"` // 明文 ts 允许的最大时效(秒), 0 或缺省=不校验
}
