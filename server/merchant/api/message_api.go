package api

import (
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

var agentMsgSvc = &cssvc.MessageService{}

// AgentMessageApi 客服工作台消息接口(全量历史)
type AgentMessageApi struct{}

func (a *AgentMessageApi) Send(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var req csReq.SendMsgReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	res, err := agentMsgSvc.Send(c.Request.Context(), claims.TenantID, "agent", claims.AccountID, convID, req)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

// History 客服侧全量历史(不受7天限制), 校验成员关系
func (a *AgentMessageApi) History(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	if _, allowed := agentConvSvc.CanAccess(claims.TenantID, "agent", claims.AccountID, convID); !allowed {
		response.NoAuth("无权访问该会话", c)
		return
	}
	var q csReq.HistoryQueryReq
	_ = c.ShouldBindQuery(&q)
	res, err := agentMsgSvc.AgentHistory(c.Request.Context(), claims.TenantID, convID, q)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

// Read 上报已读水位
func (a *AgentMessageApi) Read(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	if _, allowed := agentConvSvc.CanAccess(claims.TenantID, "agent", claims.AccountID, convID); !allowed {
		response.NoAuth("无权访问该会话", c)
		return
	}
	var req csReq.ReadReportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := agentMsgSvc.ReportRead(claims.TenantID, convID, "agent", claims.AccountID, req.LastReadSeq); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(c)
}
