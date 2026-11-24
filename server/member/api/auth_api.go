package api

import (
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	"github.com/flipped-aurora/gin-vue-admin/server/middleware"
	"github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"github.com/gin-gonic/gin"
)

var memberAuthSvc = &cs.AuthService{}
var memberTenantSvc = &cs.TenantService{}

// MemberAuthApi 用户端 H5 认证接口(仅账密登录)
type MemberAuthApi struct{}

func (a *MemberAuthApi) Login(c *gin.Context) {
	var req csReq.MemberLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := memberAuthSvc.MemberLogin(c.Request.Context(), req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "登录成功", c)
}

// TenantInfo 公开接口: H5 接入链接按商户标识展示商户名/服务状态(无需登录)
func (a *MemberAuthApi) TenantInfo(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		response.FailWithMessage("缺少商户标识", c)
		return
	}
	res, err := memberTenantSvc.TenantPublic(code)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

// Profile 返回当前已验证用户身份(证明鉴权与租户上下文来自令牌)
func (a *MemberAuthApi) Profile(c *gin.Context) {
	v, ok := c.Get(middleware.CsClaimsKey)
	if !ok {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	claims := v.(*cstoken.CsClaims)
	response.OkWithData(gin.H{
		"scope":    claims.Scope,
		"memberId": claims.AccountID,
		"tenantId": claims.TenantID,
		"username": claims.Username,
	}, c)
}
