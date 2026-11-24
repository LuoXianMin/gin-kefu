package router

import (
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/merchant/api"
	"github.com/flipped-aurora/gin-vue-admin/server/middleware"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	"github.com/gin-gonic/gin"
)

type RouterGroup struct {
	AuthRouter
}

type AuthRouter struct{}

var (
	merchantAuthApi = &api.MerchantAuthApi{}
	agentAuthApi    = &api.AgentAuthApi{}
	merchantMgtApi  = &api.MerchantManageApi{}
)

// InitMerchantAuthRouter 商户端 + 客服端认证路由(公开)
func (r *AuthRouter) InitMerchantAuthRouter(PublicGroup *gin.RouterGroup) {
	merchantPublic := PublicGroup.Group("merchant")
	{
		merchantPublic.POST("/captcha", merchantAuthApi.Captcha)
		merchantPublic.POST("/sms-code", middleware.CsRateLimit(10, time.Minute), merchantAuthApi.SendSmsCode)
		merchantPublic.POST("/login", middleware.CsRateLimit(20, time.Minute), merchantAuthApi.Login)
	}
	agentPublic := PublicGroup.Group("agent")
	{
		agentPublic.POST("/captcha", agentAuthApi.Captcha)
		agentPublic.POST("/sms-code", middleware.CsRateLimit(10, time.Minute), agentAuthApi.SendSmsCode)
		agentPublic.POST("/login", middleware.CsRateLimit(20, time.Minute), agentAuthApi.Login)
	}

	// 客服工作台鉴权路由(独立 agent 令牌 + 租户上下文)
	convApi := &api.AgentConversationApi{}
	msgApi := &api.AgentMessageApi{}
	fileApi := &api.AgentFileApi{}
	agentPrivate := PublicGroup.Group("agent")
	agentPrivate.Use(middleware.CsAuth(csmodel.ScopeAgent), middleware.CsTenantScope(csmodel.ScopeAgent))
	{
		agentPrivate.GET("/conversations/pending", convApi.Pending)
		agentPrivate.GET("/conversations/mine", convApi.MyList)
		agentPrivate.POST("/conversations/:id/claim", convApi.Claim)
		agentPrivate.POST("/conversations/:id/transfer", convApi.Transfer)
		agentPrivate.POST("/conversations/:id/close", convApi.Close)
		agentPrivate.GET("/conversations/:id", convApi.Detail)
		agentPrivate.GET("/conversations/:id/events", convApi.Events)
		agentPrivate.GET("/agents/transferable", convApi.TransferableAgents)
		agentPrivate.POST("/conversations/:id/messages", middleware.CsRateLimit(120, time.Minute), msgApi.Send)
		agentPrivate.GET("/conversations/:id/messages", msgApi.History)
		agentPrivate.POST("/conversations/:id/read", msgApi.Read)
		// 附件
		agentPrivate.POST("/conversations/:id/files", fileApi.Upload)
		agentPrivate.GET("/files/:fileId", fileApi.Info)
		agentPrivate.GET("/files/:fileId/url", fileApi.DownloadURL)
	}

	// 商户管理员私有路由(客服账号/渠道/报表): CsAuth(merchant) + 租户作域
	merchantPrivate := PublicGroup.Group("merchant/admin")
	merchantPrivate.Use(middleware.CsAuth(csmodel.ScopeMerchant), middleware.CsTenantScope(csmodel.ScopeMerchant))
	{
		merchantPrivate.GET("/agents", merchantMgtApi.AgentList)
		merchantPrivate.POST("/agents", merchantMgtApi.AgentCreate)
		merchantPrivate.PUT("/agents", merchantMgtApi.AgentUpdate)
		merchantPrivate.PUT("/agents/status", merchantMgtApi.AgentStatus)
		merchantPrivate.PUT("/agents/password", merchantMgtApi.AgentResetPwd)
		// 访客(用户端)账号管理
		merchantPrivate.GET("/members", merchantMgtApi.MemberList)
		merchantPrivate.POST("/members", merchantMgtApi.MemberCreate)
		merchantPrivate.PUT("/members", merchantMgtApi.MemberUpdate)
		merchantPrivate.PUT("/members/password", merchantMgtApi.MemberResetPwd)
		merchantPrivate.GET("/channel", merchantMgtApi.ChannelGet)
		merchantPrivate.PUT("/channel", merchantMgtApi.ChannelUpdate)
		merchantPrivate.GET("/reports/overview", merchantMgtApi.Overview)
		merchantPrivate.GET("/reports/agents", merchantMgtApi.AgentPerf)
		merchantPrivate.GET("/audits", middleware.CsRateLimit(30, time.Minute), merchantMgtApi.AuditList)
	}
}
