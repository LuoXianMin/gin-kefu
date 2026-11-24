package cs

// 登录端标识(同时作为 JWT Audience 与租户上下文校验依据)
const (
	ScopePlatform = "platform" // 平台总后台
	ScopeMerchant = "merchant" // 企业商户端
	ScopeAgent    = "agent"    // 客服工作台
	ScopeMember   = "member"   // 用户端 H5
)

// 消息发送者角色(非登录端): 机器人欢迎语等系统消息专用
const SenderBot = "bot"
