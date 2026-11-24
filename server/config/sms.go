package config

// Sms 短信验证码配置
// 生产凭证(AccessKey等)一律通过环境变量注入(Env 字段指定变量名)，不得写入仓库/日志/镜像
type Sms struct {
	Provider       string `mapstructure:"provider" json:"provider" yaml:"provider"`                // log | http
	Endpoint       string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`                // http 模式短信网关地址
	AccessKeyEnv   string `mapstructure:"access-key-env" json:"access-key-env" yaml:"access-key-env"`     // 读取 AK 的环境变量名
	SecretKeyEnv   string `mapstructure:"secret-key-env" json:"secret-key-env" yaml:"secret-key-env"`     // 读取 SK 的环境变量名
	SignName       string `mapstructure:"sign-name" json:"sign-name" yaml:"sign-name"`             // 短信签名
	TemplateCode   string `mapstructure:"template-code" json:"template-code" yaml:"template-code"` // 短信模板 Code
	CodeLength     int    `mapstructure:"code-length" json:"code-length" yaml:"code-length"`       // 验证码位数
	CodeTtlSeconds int    `mapstructure:"code-ttl-seconds" json:"code-ttl-seconds" yaml:"code-ttl-seconds"`
	SendTimeLimit  int    `mapstructure:"send-time-limit" json:"send-time-limit" yaml:"send-time-limit"` // 同一手机号发送间隔(秒)
	DailyLimit     int    `mapstructure:"daily-limit" json:"daily-limit" yaml:"daily-limit"`             // 同一手机号每日发送上限
	MaxFailCount   int    `mapstructure:"max-fail-count" json:"max-fail-count" yaml:"max-fail-count"`    // 验证码连续校验失败上限
	DevReturnCode  bool   `mapstructure:"dev-return-code" json:"dev-return-code" yaml:"dev-return-code"` // 仅开发环境: 接口直接返回验证码，严禁生产开启
}
