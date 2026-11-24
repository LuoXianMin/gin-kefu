package api

import (
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

var agentFileSvc = &cssvc.FileService{}

// AgentFileApi 客服工作台附件: 上传/状态/下载链接
type AgentFileApi struct{}

func (a *AgentFileApi) Upload(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUint(c, "id")
	if !ok {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		response.FailWithMessage("缺少文件(multipart字段名file): "+err.Error(), c)
		return
	}
	res, err := agentFileSvc.Upload(claims.TenantID, "agent", claims.AccountID, convID, fh)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

func (a *AgentFileApi) Info(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	fileID, ok := pathUint(c, "fileId")
	if !ok {
		return
	}
	res, err := agentFileSvc.Info(claims.TenantID, "agent", claims.AccountID, fileID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

func (a *AgentFileApi) DownloadURL(c *gin.Context) {
	claims := agentClaims(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	fileID, ok := pathUint(c, "fileId")
	if !ok {
		return
	}
	res, err := agentFileSvc.IssueDownloadURL(claims.TenantID, "agent", claims.AccountID, fileID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}
