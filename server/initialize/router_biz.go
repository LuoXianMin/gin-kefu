package initialize

import (
	csrouter "github.com/flipped-aurora/gin-vue-admin/server/router/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/router"
	memberRouter "github.com/flipped-aurora/gin-vue-admin/server/member/router"
	merchantRouter "github.com/flipped-aurora/gin-vue-admin/server/merchant/router"
	"github.com/gin-gonic/gin"
)

// 占位方法，保证文件可以正确加载，避免go空变量检测报错，请勿删除。
func holder(routers ...*gin.RouterGroup) {
	_ = routers
	_ = router.RouterGroupApp
}

func initBizRouter(routers ...*gin.RouterGroup) {
	privateGroup := routers[0]
	publicGroup := routers[1]

	holder(publicGroup, privateGroup)

	// 客服系统三端认证路由(与平台 GVA 权限体系隔离, 独立令牌/独立鉴权)
	var csAuthRouter csrouter.AuthRouter
	csAuthRouter.InitCsAuthRouter(publicGroup)

	var merchantAuthRouter merchantRouter.AuthRouter
	merchantAuthRouter.InitMerchantAuthRouter(publicGroup)

	var memberAuthRouter memberRouter.AuthRouter
	memberAuthRouter.InitMemberAuthRouter(publicGroup)
}
