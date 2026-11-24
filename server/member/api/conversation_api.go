package api

import (
	"strconv"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	"github.com/flipped-aurora/gin-vue-admin/server/middleware"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"github.com/gin-gonic/gin"
)

var convSvc = &cssvc.ConversationService{}

// MemberConversationApi 用户端会话接口(已进入 CsAuth(member)+租户上下文)
type MemberConversationApi struct{}

func claimsFrom(c *gin.Context) *cstoken.CsClaims {
	v, _ := c.Get(middleware.CsClaimsKey)
	claims, _ := v.(*cstoken.CsClaims)
	return claims
}

func pathUintLocal(c *gin.Context, name string) (uint, bool) {
	id64, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		response.FailWithMessage("无效的参数: "+name, c)
		return 0, false
	}
	return uint(id64), true
}

// CreateOrResume 创建或恢复未结束会话
func (a *MemberConversationApi) CreateOrResume(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var req csReq.ConversationCreateReq
	_ = c.ShouldBindJSON(&req)
	if req.Source == "" {
		req.Source = "h5"
	}
	conv, err := convSvc.CreateOrResume(claims.TenantID, claims.AccountID, req.Source, req.Title)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(convSvc.ToConversationRes(conv), c)
}

// MyConversations 我的会话列表
func (a *MemberConversationApi) MyConversations(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	var list []csmodel.CsConversation
	err := global.GVA_DB.Where("tenant_id = ? AND member_id = ?", claims.TenantID, claims.AccountID).
		Order("id desc").Limit(50).Find(&list).Error
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	out := make([]interface{}, 0, len(list))
	for i := range list {
		out = append(out, convSvc.ToConversationRes(&list[i]))
	}
	response.OkWithData(out, c)
}
