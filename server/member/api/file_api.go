package api

import (
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

var memberFileSvc = &cssvc.FileService{}

// MemberFileApi 用户端附件: 上传/状态/下载链接
type MemberFileApi struct{}

func (a *MemberFileApi) Upload(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	convID, ok := pathUintLocal(c, "id")
	if !ok {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		response.FailWithMessage("缺少文件(multipart字段名file): "+err.Error(), c)
		return
	}
	res, err := memberFileSvc.Upload(claims.TenantID, "member", claims.AccountID, convID, fh)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

func (a *MemberFileApi) Info(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	fileID, ok := pathUintLocal(c, "fileId")
	if !ok {
		return
	}
	res, err := memberFileSvc.Info(claims.TenantID, "member", claims.AccountID, fileID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}

func (a *MemberFileApi) DownloadURL(c *gin.Context) {
	claims := claimsFrom(c)
	if claims == nil {
		response.NoAuth("缺少身份上下文", c)
		return
	}
	fileID, ok := pathUintLocal(c, "fileId")
	if !ok {
		return
	}
	res, err := memberFileSvc.IssueDownloadURL(claims.TenantID, "member", claims.AccountID, fileID)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.OkWithData(res, c)
}
