package api

import (
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	"github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

var (
	authSvc    = &cs.AuthService{}
	captchaSvc = &cs.CaptchaService{}
)

// MerchantAuthApi 企业商户端(商户管理员)认证接口
type MerchantAuthApi struct{}

func (a *MerchantAuthApi) Captcha(c *gin.Context) {
	id, b64s, answer, err := captchaSvc.Generate()
	if err != nil {
		response.FailWithMessage("验证码获取失败", c)
		return
	}
	response.OkWithDetailed(gin.H{"captchaId": id, "picPath": b64s, "devCaptcha": answer}, "获取成功", c)
}

func (a *MerchantAuthApi) SendSmsCode(c *gin.Context) {
	var req csReq.SendSmsCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := authSvc.SendSmsCode(c.Request.Context(), csmodel.ScopeMerchant, req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "验证码已发送", c)
}

func (a *MerchantAuthApi) Login(c *gin.Context) {
	var req csReq.MfaLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := authSvc.MerchantLogin(c.Request.Context(), req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "登录成功", c)
}

// AgentAuthApi 客服工作台(客服/主管)认证接口
type AgentAuthApi struct{}

func (a *AgentAuthApi) Captcha(c *gin.Context) {
	id, b64s, answer, err := captchaSvc.Generate()
	if err != nil {
		response.FailWithMessage("验证码获取失败", c)
		return
	}
	response.OkWithDetailed(gin.H{"captchaId": id, "picPath": b64s, "devCaptcha": answer}, "获取成功", c)
}

func (a *AgentAuthApi) SendSmsCode(c *gin.Context) {
	var req csReq.SendSmsCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := authSvc.SendSmsCode(c.Request.Context(), csmodel.ScopeAgent, req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "验证码已发送", c)
}

func (a *AgentAuthApi) Login(c *gin.Context) {
	var req csReq.MfaLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := authSvc.AgentLogin(c.Request.Context(), req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "登录成功", c)
}
