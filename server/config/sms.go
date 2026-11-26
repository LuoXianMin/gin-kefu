package config

// Sms 短信验证码配置
// 生产凭证(AccessKey等)一律通过环境变量注入(Env 字段指定变量名)，不得写入仓库/日志/镜像
type Sms struct {
	Provider         string `mapstructure:"provider" json:"provider" yaml:"provider"`                               // log | http | aliyun
	Endpoint         string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`                               // http 模式必填网关地址; aliyun 模式可留空(默认 https://dysmsapi.aliyuncs.com)
	AccessKeyEnv     string `mapstructure:"access-key-env" json:"access-key-env" yaml:"access-key-env"`             // 读取 AK 的环境变量名
	SecretKeyEnv     string `mapstructure:"secret-key-env" json:"secret-key-env" yaml:"secret-key-env"`             // 读取 SK 的环境变量名
	SignName         string `mapstructure:"sign-name" json:"sign-name" yaml:"sign-name"`                            // 短信签名
	TemplateCode     string `mapstructure:"template-code" json:"template-code" yaml:"template-code"`                // 短信模板 Code
	Region           string `mapstructure:"region" json:"region" yaml:"region"`                                     // aliyun 地域, 留空=cn-hangzhou
	TemplateParamKey string `mapstructure:"template-param-key" json:"template-param-key" yaml:"template-param-key"` // aliyun 模板验证码变量名, 留空=code
	CodeLength       int    `mapstructure:"code-length" json:"code-length" yaml:"code-length"`                      // 验证码位数
	CodeTtlSeconds   int    `mapstructure:"code-ttl-seconds" json:"code-ttl-seconds" yaml:"code-ttl-seconds"`
	SendTimeLimit    int    `mapstructure:"send-time-limit" json:"send-time-limit" yaml:"send-time-limit"` // 同一手机号发送间隔(秒)
	DailyLimit       int    `mapstructure:"daily-limit" json:"daily-limit" yaml:"daily-limit"`             // 同一手机号每日发送上限
	MaxFailCount     int    `mapstructure:"max-fail-count" json:"max-fail-count" yaml:"max-fail-count"`    // 验证码连续校验失败上限
	DevReturnCode    bool   `mapstructure:"dev-return-code" json:"dev-return-code" yaml:"dev-return-code"` // 仅开发环境: 接口直接返回验证码，严禁生产开启
}
