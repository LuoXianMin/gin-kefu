package cs

import (
	"strconv"

	"github.com/flipped-aurora/gin-vue-admin/server/middleware"
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	"github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"github.com/gin-gonic/gin"
)

var (
	tenantAdminSvc    = &cs.TenantAdminService{}
	platformReportSvc = &cs.PlatformReportService{}
)

// PlatformAdminApi 平台总后台: 租户(商户)管理。所有接口经 CsAuth(platform) 保护。
type PlatformAdminApi struct{}

func platformClaims(c *gin.Context) *cstoken.CsClaims {
	v, _ := c.Get(middleware.CsClaimsKey)
	claims, _ := v.(*cstoken.CsClaims)
	return claims
}

func (a *PlatformAdminApi) List(c *gin.Context) {
	if platformClaims(c) == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var q csReq.TenantSearchReq
	_ = c.ShouldBindQuery(&q)
	list, total, err := tenantAdminSvc.List(q)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(response.PageResult{List: list, Total: total, Page: q.Page, PageSize: q.PageSize}, "获取成功", c)
}

func (a *PlatformAdminApi) Create(c *gin.Context) {
	if platformClaims(c) == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.TenantCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := tenantAdminSvc.Create(req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	claims := platformClaims(c)
	cs.Audit(0, csmodel.ScopePlatform, claims.AccountID, "tenant.create", "tenant:"+strconv.FormatUint(uint64(res.ID), 10), "code="+res.Code, c.ClientIP())
	response.OkWithData(res, c)
}

func (a *PlatformAdminApi) Update(c *gin.Context) {
	if platformClaims(c) == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.TenantUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := tenantAdminSvc.Update(req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	claims := platformClaims(c)
	cs.Audit(0, csmodel.ScopePlatform, claims.AccountID, "tenant.update", "tenant:"+strconv.FormatUint(uint64(req.ID), 10), "name="+req.Name, c.ClientIP())
	response.Ok(c)
}

func (a *PlatformAdminApi) SetStatus(c *gin.Context) {
	if platformClaims(c) == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.TenantStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := tenantAdminSvc.SetStatus(req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	claims := platformClaims(c)
	cs.Audit(0, csmodel.ScopePlatform, claims.AccountID, "tenant.status", "tenant:"+strconv.FormatUint(uint64(req.ID), 10), "status="+strconv.Itoa(req.Status), c.ClientIP())
	response.Ok(c)
}

// Delete 注销删除商户(软删租户+账号, 有未结束会话拒绝)
func (a *PlatformAdminApi) Delete(c *gin.Context) {
	if platformClaims(c) == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.TenantDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := tenantAdminSvc.Delete(req.ID); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	claims := platformClaims(c)
	cs.Audit(0, csmodel.ScopePlatform, claims.AccountID, "tenant.delete", "tenant:"+strconv.FormatUint(uint64(req.ID), 10), "", c.ClientIP())
	response.Ok(c)
}

// AuditList 全局审计查询(平台侧); 本次访问自身也落审计
func (a *PlatformAdminApi) AuditList(c *gin.Context) {
	claims := platformClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var q csReq.AuditSearchReq
	_ = c.ShouldBindQuery(&q)
	list, total, err := cs.ListAudits(q.TenantID, q.Action, q.Page, q.PageSize)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cs.Audit(0, csmodel.ScopePlatform, claims.AccountID, "audit.query", "audits", "tenantId="+strconv.FormatUint(uint64(q.TenantID), 10)+" action="+q.Action, c.ClientIP())
	response.OkWithDetailed(response.PageResult{List: list, Total: total, Page: q.Page, PageSize: q.PageSize}, "获取成功", c)
}

// Overview 平台聚合运营指标(租户数/会话量/消息量/在线客服)
func (a *PlatformAdminApi) Overview(c *gin.Context) {
	if platformClaims(c) == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	res, err := platformReportSvc.Overview(c.Request.Context())
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}
