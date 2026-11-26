package cs

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csModel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/upload"
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
	// FilePath 的形态由 system.oss-type 决定: local 为容器内相对路径, 阿里云 OSS 等为公网完整 URL
	// (utils/upload/aliyun_oss.go:40 返回 BucketUrl+"/"+key)。鉴权与时效已在上一步校验完毕,
	// 这里只按形态分流, 历史 local 数据无需迁移。
	if isRemoteURL(file.FilePath) {
		c.Redirect(http.StatusFound, remoteDownloadTarget(file, exp))
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(file.FileName))
	c.Header("Cache-Control", "private, no-store")
	c.File(file.FilePath)
}

// isRemoteURL 判别存储后端返回的是外链还是本地文件路径
func isRemoteURL(p string) bool {
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

// remoteDownloadTarget 生成 302 目标。首选带时效的 OSS 签名直链(时长对齐短链剩余寿命),
// 否则公共读裸链会绕过上面的 HMAC 时效控制。图片不附 disposition 以便命中 OSS/CDN 缓存;
// 其余附件附 disposition 保住"另存为 + 原始文件名"(重定向后响应头由 OSS 给出,
// 本函数先前设置的 Content-Disposition 不会跟随到最终响应)。
// 后端不具备签名能力时(如 minio)回退裸链 + query 形式的 response-content-disposition。
func remoteDownloadTarget(file *csModel.CsFileAttachment, exp int64) string {
	disposition := ""
	if !strings.HasPrefix(file.MimeType, "image/") {
		disposition = "attachment; filename*=UTF-8''" + url.PathEscape(file.FileName)
	}
	if signed, err := upload.SignRemoteURL(file.StoredName, remainingTTL(exp), disposition); err == nil && signed != "" {
		return signed
	}
	if disposition == "" {
		return file.FilePath
	}
	return file.FilePath + "?response-content-disposition=" + url.QueryEscape(disposition)
}

// remainingTTL 以短链剩余有效期为签名时长, 避免 302 目标比短链本身活得更久
func remainingTTL(exp int64) time.Duration {
	if d := time.Until(time.Unix(exp, 0)); d > 0 {
		return d
	}
	return time.Second
}
