package initialize

import (
	"context"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	sysModel "github.com/flipped-aurora/gin-vue-admin/server/model/system"
	csservice "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"go.uber.org/zap"
)

// CsInit 客服系统: 建表 + 演示数据种子(幂等)。在路由注册前调用。
func CsInit() {
	if global.GVA_DB == nil {
		return
	}
	tables := []interface{}{
		&csmodel.CsTenant{},
		&csmodel.CsAgent{},
		&csmodel.CsMember{},
		&csmodel.CsLoginFailure{},
		&csmodel.CsConversation{},
		&csmodel.CsConversationMember{},
		&csmodel.CsAssignmentEvent{},
		&csmodel.CsFileAttachment{},
		&csmodel.CsAuditLog{},
		&csservice.CsReadCursor{},
	}
	for _, t := range tables {
		if err := global.GVA_DB.AutoMigrate(t); err != nil {
			global.GVA_LOG.Error("cs automigrate failed", zap.Error(err))
		}
	}
	// Mongo 消息集合索引(异步落库/查询主存储), 失败仅告警不阻断启动
	if global.GVA_MONGO != nil {
		if err := csservice.EnsureMessageIndexes(context.Background()); err != nil {
			global.GVA_LOG.Warn("cs mongo index failed", zap.Error(err))
		}
	}
	seedCsDemo()
}

// seedCsDemo 若演示租户不存在则创建一组可登录账号(仅开发/演示)
func seedCsDemo() {
	var cnt int64
	global.GVA_DB.Model(&csmodel.CsTenant{}).Where("code = ?", "demo").Count(&cnt)
	if cnt > 0 {
		return
	}

	pwd, err := csservice.HashPassword("123456")
	if err != nil {
		global.GVA_LOG.Error("cs seed hash password failed", zap.Error(err))
		return
	}

	tenant := csmodel.CsTenant{
		Code: "demo", Name: "示例商户", Status: csmodel.TenantStatusNormal,
		Package: "basic", AgentHardLimit: 8, WelcomeTemplate: "您好，很高兴为您服务",
	}
	if err := global.GVA_DB.Create(&tenant).Error; err != nil {
		global.GVA_LOG.Error("cs seed tenant failed", zap.Error(err))
		return
	}

	agents := []csmodel.CsAgent{
		{TenantID: tenant.ID, Username: "merchantadmin", Password: pwd, Phone: "13800000001",
			RealName: "商户管理员", EmployeeNo: "M001", Role: csmodel.AgentRoleMerchantAdmin,
			Enable: csmodel.AccountStatusNormal, MaxActive: 8},
		{TenantID: tenant.ID, Username: "agent001", Password: pwd, Phone: "13800000002",
			RealName: "张三", EmployeeNo: "008", Role: csmodel.AgentRoleAgent,
			Enable: csmodel.AccountStatusNormal, MaxActive: 8},
	}
	if err := global.GVA_DB.Create(&agents).Error; err != nil {
		global.GVA_LOG.Error("cs seed agents failed", zap.Error(err))
		return
	}

	members := []csmodel.CsMember{
		{TenantID: tenant.ID, Username: "member001", Password: pwd, NickName: "访客小王",
			Phone: "13700000001", Source: "H5", Status: csmodel.AccountStatusNormal},
	}
	if err := global.GVA_DB.Create(&members).Error; err != nil {
		global.GVA_LOG.Error("cs seed members failed", zap.Error(err))
		return
	}

	// 为平台端 MFA 登录准备手机号: 若默认 admin 无手机号则补一个(仅演示)
	var admin sysModel.SysUser
	if err := global.GVA_DB.Where("username = ?", "admin").First(&admin).Error; err == nil {
		if admin.Phone == "" {
			global.GVA_DB.Model(&admin).Update("phone", "13900000000")
		}
	}
	global.GVA_LOG.Info("cs demo data seeded", zap.Uint("tenantId", tenant.ID))
}
