package core

import (
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/flipped-aurora/gin-vue-admin/server/initialize"
	"go.uber.org/zap"
)

// WorkerInit 为独立后台程序(storage-worker / ws-gateway)初始化最小依赖集:
// 配置(Viper) + 日志(Zap) + 主库(Gorm) + Redis + Mongo。按 config 开关条件连接。
// 与主服务 RunServer 区别: 不注册 HTTP 路由、不启动 gin、不加载 casbin/菜单。
func WorkerInit() {
	global.GVA_VP = Viper()
	global.GVA_LOG = Zap()
	zap.ReplaceGlobals(global.GVA_LOG)

	if !global.GVA_CONFIG.System.UseRedis {
		panic("worker 需要 Redis(幂等/seq), 请在 config 中开启 system.use-redis")
	}
	initialize.Redis()

	global.GVA_DB = initialize.Gorm()
	if global.GVA_DB == nil {
		panic("worker 需要主数据库连接")
	}

	if global.GVA_CONFIG.System.UseMongo {
		if err := initialize.Mongo.Initialization(); err != nil {
			global.GVA_LOG.Error("worker mongo init failed", zap.Error(err))
		}
	} else {
		panic("worker 需要 MongoDB(消息落库/查询), 请开启 system.use-mongo")
	}

	if !global.GVA_CONFIG.Kafka.Enabled {
		panic("worker 需要 Kafka, 请开启 kafka.enabled")
	}
}
