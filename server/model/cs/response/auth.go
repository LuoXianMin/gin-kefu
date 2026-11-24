package response

// SendSmsCodeRes 短信验证码发送结果
// DevCode 仅在 sms.dev-return-code=true 的开发环境返回，生产恒为空
type SendSmsCodeRes struct {
	ExpireSeconds int    `json:"expireSeconds"`
	DevCode       string `json:"devCode,omitempty"`
}

// CsTokenRes 统一登录响应
type CsTokenRes struct {
	Token   string `json:"token"`
	Expires int64  `json:"expires"`
}

// PlatformLoginRes 平台端登录响应
type PlatformLoginRes struct {
	CsTokenRes
	CsUser *PlatformUserInfo `json:"csUser"`
}

type PlatformUserInfo struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	NickName    string `json:"nickName"`
	AuthorityId uint   `json:"authorityId"`
	Phone       string `json:"phone"` // 已脱敏
}

// MerchantLoginRes 商户端登录响应
type MerchantLoginRes struct {
	CsTokenRes
	Tenant *TenantInfo `json:"tenant"`
	Admin  *AgentInfo  `json:"admin"`
}

// AgentLoginRes 客服端登录响应
type AgentLoginRes struct {
	CsTokenRes
	Tenant *TenantInfo `json:"tenant"`
	Agent  *AgentInfo  `json:"agent"`
}

type TenantInfo struct {
	ID   uint   `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// AgentInfo 客服/商户管理员信息 真名不下发给用户侧场景，商户/客服端仅见本人或有权数据
type AgentInfo struct {
	ID         uint   `json:"id"`
	TenantID   uint   `json:"tenantId"`
	Username   string `json:"username"`
	EmployeeNo string `json:"employeeNo"`
	Role       string `json:"role"`
	MaxActive  int    `json:"maxActive"`
	Phone      string `json:"phone"` // 已脱敏
}

// MemberLoginRes 用户端登录响应
type MemberLoginRes struct {
	CsTokenRes
	Tenant *TenantInfo `json:"tenant"`
	Member *MemberInfo `json:"member"`
}

type MemberInfo struct {
	ID         uint   `json:"id"`
	TenantID   uint   `json:"tenantId"`
	Username   string `json:"username"`
	NickName   string `json:"nickName"`
	Source     string `json:"source"`
}
