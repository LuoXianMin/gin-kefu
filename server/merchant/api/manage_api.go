package api

import (
	"strconv"

	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

var (
	agentAdminSvc  = &cssvc.AgentAdminService{}
	memberAdminSvc = &cssvc.MemberAdminService{}
	channelSvc     = &cssvc.ChannelService{}
	reportSvc      = &cssvc.ReportService{}
)

// MerchantManageApi 商户管理员: 客服账号 / H5渠道 / 报表
type MerchantManageApi struct{}

func (a *MerchantManageApi) AgentList(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var q csReq.AgentSearchReq
	_ = c.ShouldBindQuery(&q)
	list, total, err := agentAdminSvc.List(claims.TenantID, q)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(response.PageResult{List: list, Total: total, Page: q.Page, PageSize: q.PageSize}, "获取成功", c)
}

func (a *MerchantManageApi) AgentCreate(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.AgentCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := agentAdminSvc.Create(claims.TenantID, req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.Audit(claims.TenantID, "merchant", claims.AccountID, "agent.create", "agent:"+itoa(res.ID), "username="+res.Username+" role="+res.Role, c.ClientIP())
	response.OkWithData(res, c)
}

func (a *MerchantManageApi) AgentUpdate(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.AgentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := agentAdminSvc.Update(claims.TenantID, req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(c)
}

func (a *MerchantManageApi) AgentStatus(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.AgentStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := agentAdminSvc.SetStatus(claims.TenantID, claims.AccountID, req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.Audit(claims.TenantID, "merchant", claims.AccountID, "agent.status", "agent:"+itoa(req.ID), "enable="+strconv.Itoa(req.Enable), c.ClientIP())
	response.Ok(c)
}

func (a *MerchantManageApi) AgentResetPwd(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.AgentResetPwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := agentAdminSvc.ResetPassword(claims.TenantID, req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.Audit(claims.TenantID, "merchant", claims.AccountID, "agent.reset_password", "agent:"+itoa(req.ID), "", c.ClientIP())
	response.Ok(c)
}

func itoa(v uint) string { return strconv.FormatUint(uint64(v), 10) }

// ChannelGet 读取本租户 H5 渠道配置
func (a *MerchantManageApi) ChannelGet(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	res, err := channelSvc.GetConfig(claims.TenantID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

func (a *MerchantManageApi) ChannelUpdate(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.ChannelConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := channelSvc.UpdateConfig(claims.TenantID, req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(c)
}

func (a *MerchantManageApi) Overview(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	res, err := reportSvc.Overview(claims.TenantID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

func (a *MerchantManageApi) AgentPerf(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	list, err := reportSvc.AgentPerformance(claims.TenantID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(list, c)
}

// AuditList 本租户审计查询(商户侧强制 tenant 隔离, 不得跨租户)
func (a *MerchantManageApi) AuditList(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var q csReq.AuditSearchReq
	_ = c.ShouldBindQuery(&q)
	list, total, err := cssvc.ListAudits(claims.TenantID, q.Action, q.Page, q.PageSize)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.Audit(claims.TenantID, "merchant", claims.AccountID, "audit.query", "audits", "action="+q.Action, c.ClientIP())
	response.OkWithDetailed(response.PageResult{List: list, Total: total, Page: q.Page, PageSize: q.PageSize}, "获取成功", c)
}

// ---- 访客(用户端)账号管理: 严格限本租户 ----

func (a *MerchantManageApi) MemberList(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var q csReq.MemberSearchReq
	_ = c.ShouldBindQuery(&q)
	list, total, err := memberAdminSvc.List(claims.TenantID, q)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(response.PageResult{List: list, Total: total, Page: q.Page, PageSize: q.PageSize}, "获取成功", c)
}

func (a *MerchantManageApi) MemberCreate(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.MemberCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := memberAdminSvc.Create(claims.TenantID, req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.Audit(claims.TenantID, "merchant", claims.AccountID, "member.create", strconv.FormatUint(uint64(res.ID), 10), req.Username, c.ClientIP())
	response.OkWithData(res, c)
}

func (a *MerchantManageApi) MemberUpdate(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.MemberUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := memberAdminSvc.Update(claims.TenantID, req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.Audit(claims.TenantID, "merchant", claims.AccountID, "member.update", strconv.FormatUint(uint64(req.ID), 10), "", c.ClientIP())
	response.Ok(c)
}

func (a *MerchantManageApi) MemberResetPwd(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.MemberResetPwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := memberAdminSvc.ResetPwd(claims.TenantID, req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.Audit(claims.TenantID, "merchant", claims.AccountID, "member.resetpwd", strconv.FormatUint(uint64(req.ID), 10), "", c.ClientIP())
	response.Ok(c)
}
