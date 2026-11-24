package router

import (
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/member/api"
	"github.com/flipped-aurora/gin-vue-admin/server/middleware"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	"github.com/gin-gonic/gin"
)

type RouterGroup struct {
	AuthRouter
}

type AuthRouter struct{}

var memberAuthApi = &api.MemberAuthApi{}

// InitMemberAuthRouter 用户端路由: 公开登录 + 用户令牌鉴权的会话入口
// 使用未挂 GVA JWT 的 PublicGroup 作为基座, 由 CsAuth 独立鉴权, 与平台端令牌隔离
func (r *AuthRouter) InitMemberAuthRouter(PublicGroup *gin.RouterGroup) {
	memberPublic := PublicGroup.Group("member")
	{
		memberPublic.POST("/login", middleware.CsRateLimit(10, time.Minute), memberAuthApi.Login)
		// 接入链接预查询: 按商户标识展示商户名/状态(公开)
		memberPublic.GET("/tenant", middleware.CsRateLimit(30, time.Minute), memberAuthApi.TenantInfo)
	}
	memberPrivate := PublicGroup.Group("member")
	memberPrivate.Use(middleware.CsAuth(csmodel.ScopeMember), middleware.CsTenantScope(csmodel.ScopeMember))
	{
		memberPrivate.GET("/profile", memberAuthApi.Profile)
	}

	// 会话与消息(需登录)
	convApi := &api.MemberConversationApi{}
	msgApi := &api.MemberMessageApi{}
	fileApi := &api.MemberFileApi{}
	{
		memberPrivate.POST("/conversations", convApi.CreateOrResume)
		memberPrivate.GET("/conversations", convApi.MyConversations)
		memberPrivate.POST("/conversations/:id/messages", middleware.CsRateLimit(60, time.Minute), msgApi.Send)
		memberPrivate.GET("/conversations/:id/messages", msgApi.History)
		memberPrivate.POST("/conversations/:id/read", msgApi.Read)
		// 附件: 上传/状态/短时效下载链接
		memberPrivate.POST("/conversations/:id/files", fileApi.Upload)
		memberPrivate.GET("/files/:fileId", fileApi.Info)
		memberPrivate.GET("/files/:fileId/url", fileApi.DownloadURL)
	}
}
