package request

// SendSmsCodeReq 获取短信验证码
// 安全: 手机号不由客户端任意指定，服务端按 scope+账号(tenantCode/username) 反查绑定手机号
type SendSmsCodeReq struct {
	Username   string `json:"username" binding:"required"`     // 登录账号
	TenantCode string `json:"tenantCode"`                      // merchant 端必填: 租户编码
	Captcha    string `json:"captcha" binding:"required"`      // 图形验证码
	CaptchaId  string `json:"captchaId" binding:"required"`    // 图形验证码ID
}

// MfaLoginReq 平台/商户/客服端登录: 账密 + 图形验证码 + 短信验证码
type MfaLoginReq struct {
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required"`
	Captcha    string `json:"captcha" binding:"required"`
	CaptchaId  string `json:"captchaId" binding:"required"`
	SmsCode    string `json:"smsCode" binding:"required,len=6"`
	TenantCode string `json:"tenantCode"` // merchant 端必填
}

// MemberLoginReq 用户端登录: 仅账密; tenantCode 为 H5 接入链接携带的商户标识(可选, 校验账号归属一致)
type MemberLoginReq struct {
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required"`
	TenantCode string `json:"tenantCode" binding:"max=64"`
}
