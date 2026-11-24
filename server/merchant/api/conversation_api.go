package api

import (
	"fmt"
	"strconv"

	"github.com/flipped-aurora/gin-vue-admin/server/middleware"
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"github.com/gin-gonic/gin"
)

var (
	agentConvSvc = &cssvc.ConversationService{}
)

// AgentConversationApi 客服工作台会话接口
type AgentConversationApi struct{}

func agentClaims(c *gin.Context) *cstoken.CsClaims {
	v, _ := c.Get(middleware.CsClaimsKey)
	claims, _ := v.(*cstoken.CsClaims)
	return claims
}

func pathUint(c *gin.Context, name string) (uint, bool) {
	id64, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		response.FailWithMessage("无效的参数: "+name, c)
		return 0, false
	}
	return uint(id64), true
}

// Pending 待接入列表
func (a *AgentConversationApi) Pending(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	list, total, err := agentConvSvc.ListPending(claims.TenantID, 50, 0)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	out := make([]csRes.ConversationRes, 0, len(list))
	for i := range list {
		out = append(out, agentConvSvc.ToConversationRes(&list[i]))
	}
	agentConvSvc.AttachMemberInfo(claims.TenantID, out) // 列表展示访客用户名+脱敏手机号
	response.OkWithDetailed(gin.H{"list": out, "total": total}, "获取成功", c)
}

// Claim 原子抢单(同会话仅一人接入, 不超上限)
func (a *AgentConversationApi) Claim(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	conv, err := agentConvSvc.Claim(claims.TenantID, claims.AccountID, convID)
	if err != nil {
		cssvc.MetricIncr(c.Request.Context(), cssvc.MetricClaimDenied, err.Error())
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.MetricIncr(c.Request.Context(), cssvc.MetricConvClaimed, "")
	response.OkWithData(agentConvSvc.ToConversationRes(conv), c)
}

// Transfer 转接给有权限客服
func (a *AgentConversationApi) Transfer(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var req csReq.TransferReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := agentConvSvc.Transfer(claims.TenantID, claims.AccountID, convID, req.ToAgentID, req.Reason); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cssvc.MetricIncr(c.Request.Context(), cssvc.MetricTransferDone, "")
	cssvc.Audit(claims.TenantID, csmodel.ScopeAgent, claims.AccountID, "conv.transfer",
		fmt.Sprintf("conv:%d", convID), fmt.Sprintf("to_agent=%d reason=%s", req.ToAgentID, req.Reason), c.ClientIP())
	response.OkWithMessage("转接成功", c)
}

// Close 结束会话
func (a *AgentConversationApi) Close(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	var req csReq.CloseReq
	_ = c.ShouldBindJSON(&req)
	if err := agentConvSvc.Close(claims.TenantID, claims.AccountID, convID, req.Reason); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithMessage("会话已结束", c)
}

// Detail 会话详情(含侧栏用户资料, 校验成员关系防 IDOR)
func (a *AgentConversationApi) Detail(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	conv, allowed := agentConvSvc.CanAccess(claims.TenantID, "agent", claims.AccountID, convID)
	if !allowed {
		response.NoAuth("无权访问该会话", c)
		return
	}
	sidebar, _ := agentConvSvc.UserSidebar(conv.TenantID, conv.MemberID)
	response.OkWithData(gin.H{
		"conversation": agentConvSvc.ToConversationRes(conv),
		"user":         sidebar,
	}, c)
}

// Events 本会话接待轨迹
func (a *AgentConversationApi) Events(c *gin.Context) {
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
	events, err := agentConvSvc.Events(claims.TenantID, convID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(events, c)
}

// MyList 我接待中的会话 + 计数
func (a *AgentConversationApi) MyList(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	list, total, err := agentConvSvc.ListByAgent(claims.TenantID, claims.AccountID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	out := make([]csRes.ConversationRes, 0, len(list))
	for i := range list {
		out = append(out, agentConvSvc.ToConversationRes(&list[i]))
	}
	agentConvSvc.AttachMemberInfo(claims.TenantID, out) // 列表展示访客用户名+脱敏手机号
	response.OkWithDetailed(gin.H{"list": out, "active": total}, "获取成功", c)
}

// TransferableAgents 同租户可转接客服(供工作台转接下拉)
func (a *AgentConversationApi) TransferableAgents(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	list, err := agentConvSvc.TransferableAgents(claims.TenantID, claims.AccountID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(list, c)
}
