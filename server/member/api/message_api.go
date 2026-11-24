package api

import (
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

var msgSvc = &cssvc.MessageService{}

// MemberMessageApi 用户端消息接口(7天窗口/发送/已读)
type MemberMessageApi struct{}

func (a *MemberMessageApi) Send(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUintLocal(c, "id")
	if !ok {
		return
	}
	var req csReq.SendMsgReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := msgSvc.Send(c.Request.Context(), claims.TenantID, "member", claims.AccountID, convID, req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

// History 最近7天历史(服务端强制窗口)
func (a *MemberMessageApi) History(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUintLocal(c, "id")
	if !ok {
		return
	}
	// 会话成员校验(防越权读他人会话)
	if _, allowed := convSvc.CanAccess(claims.TenantID, "member", claims.AccountID, convID); !allowed {
		response.NoAuth("无权访问该会话", c)
		return
	}
	var q csReq.HistoryQueryReq
	_ = c.ShouldBindQuery(&q)
	res, err := msgSvc.MemberHistory(c.Request.Context(), claims.TenantID, convID, q)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

// Read 上报已读水位
func (a *MemberMessageApi) Read(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUintLocal(c, "id")
	if !ok {
		return
	}
	if _, allowed := convSvc.CanAccess(claims.TenantID, "member", claims.AccountID, convID); !allowed {
		response.NoAuth("无权访问该会话", c)
		return
	}
	var req csReq.ReadReportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := msgSvc.ReportRead(claims.TenantID, convID, "member", claims.AccountID, req.LastReadSeq); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(c)
}
