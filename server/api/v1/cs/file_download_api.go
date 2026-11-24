package cs

import (
	"net/url"
	"strconv"

	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	"github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

var fileDownloadSvc = &cs.FileService{}

// FileDownloadApi 短时效签名下载(公开路径, 依赖 HMAC 签名鉴权, 链接5分钟过期)
type FileDownloadApi struct{}

func (a *FileDownloadApi) Download(c *gin.Context) {
	fid, err1 := strconv.ParseUint(c.Query("fid"), 10, 64)
	exp, err2 := strconv.ParseInt(c.Query("exp"), 10, 64)
	sig := c.Query("sig")
	if err1 != nil || err2 != nil || sig == "" {
		response.FailWithMessage("无效下载链接", c)
		return
	}
	file, err := fileDownloadSvc.ResolveDownload(uint(fid), exp, sig)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(file.FileName))
	c.Header("Cache-Control", "private, no-store")
	c.File(file.FilePath)
}
