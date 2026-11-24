package cs

import (
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	systemReq "github.com/flipped-aurora/gin-vue-admin/server/model/system/request"
	"github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

// PlatformAuthApi 平台总后台客服域认证接口
type PlatformAuthApi struct{}

var (
	platformAuthService  = &cs.AuthService{}
	platformCaptchaService = &cs.CaptchaService{}
)

// Captcha 生成图形验证码
func (a *PlatformAuthApi) Captcha(c *gin.Context) {
	id, b64s, answer, err := platformCaptchaService.Generate()
	if err != nil {
		response.FailWithMessage("验证码获取失败", c)
		return
	}
	response.OkWithDetailed(gin.H{"captchaId": id, "picPath": b64s, "devCaptcha": answer}, "获取成功", c)
}

// SendSmsCode 获取短信验证码(需先通过图形验证码)
func (a *PlatformAuthApi) SendSmsCode(c *gin.Context) {
	var req csReq.SendSmsCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := platformAuthService.SendSmsCode(c.Request.Context(), csmodel.ScopePlatform, req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "验证码已发送", c)
}

// Login 平台端登录: 账密 + 图形验证码 + 短信验证码
func (a *PlatformAuthApi) Login(c *gin.Context) {
	var req csReq.MfaLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := platformAuthService.PlatformLogin(c.Request.Context(), req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "登录成功", c)
}

// Exchange 用框架(GVA)已登录态免 MFA 换取 cs 平台令牌; 路由仅挂框架 JWTAuth, claims 必存在
func (a *PlatformAuthApi) Exchange(c *gin.Context) {
	v, _ := c.Get("claims")
	claims, _ := v.(*systemReq.CustomClaims)
	if claims == nil {
		response.NoAuth("缺少框架登录态", c)
		return
	}
	res, err := platformAuthService.PlatformExchange(claims.BaseClaims.ID, claims.BaseClaims.AuthorityId)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithDetailed(res, "获取成功", c)
}
