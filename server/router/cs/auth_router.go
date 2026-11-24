package cs

import (
	"time"

	csapi "github.com/flipped-aurora/gin-vue-admin/server/api/v1/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/middleware"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	"github.com/gin-gonic/gin"
)

type RouterGroup struct {
	AuthRouter
}

type AuthRouter struct{}

var (
	platformAuthApi  = &csapi.PlatformAuthApi{}
	platformAdminApi = &csapi.PlatformAdminApi{}
	fileDownloadApi  = &csapi.FileDownloadApi{}
	metricsApi       = &csapi.MetricsApi{}
)

// InitCsAuthRouter 平台总后台客服域认证路由(公开) + 租户管理路由(平台令牌保护)
func (r *AuthRouter) InitCsAuthRouter(PublicGroup *gin.RouterGroup) {
	csPublic := PublicGroup.Group("cs")
	{
		csPublic.GET("/metrics", metricsApi.Exposure)
		csPublic.POST("/platform/captcha", platformAuthApi.Captcha)
		csPublic.POST("/platform/sms-code", middleware.CsRateLimit(10, time.Minute), platformAuthApi.SendSmsCode)
		csPublic.POST("/platform/login", middleware.CsRateLimit(20, time.Minute), platformAuthApi.Login)
	}
	// GVA 后台已登录态免二次 MFA 换平台令牌: 仅挂框架 JWTAuth(不走 Casbin, 避免未录入 API 被 403)
	fwExchange := PublicGroup.Group("cs/platform")
	fwExchange.Use(middleware.JWTAuth())
	{
		fwExchange.POST("/exchange", platformAuthApi.Exchange)
	}
	// 附件短时效签名下载(公开路径, HMAC 签名鉴权)
	PublicGroup.Group("cs").GET("/files/dl", fileDownloadApi.Download)
	// 平台管理私有路由: 从 publicGroup 再派生, 只挂 CsAuth(platform), 不复用框架 PrivateGroup(避免 JWTAuth+Casbin 双重鉴权)
	adminPrivate := PublicGroup.Group("cs/platform/admin")
	adminPrivate.Use(middleware.CsAuth(csmodel.ScopePlatform))
	{
		adminPrivate.GET("/tenants", platformAdminApi.List)
		adminPrivate.POST("/tenants", platformAdminApi.Create)
		adminPrivate.PUT("/tenants", platformAdminApi.Update)
		adminPrivate.PUT("/tenants/status", platformAdminApi.SetStatus)
		adminPrivate.PUT("/tenants/delete", platformAdminApi.Delete)
		adminPrivate.GET("/audits", middleware.CsRateLimit(30, time.Minute), platformAdminApi.AuditList)
		adminPrivate.GET("/overview", platformAdminApi.Overview)
	}
}
