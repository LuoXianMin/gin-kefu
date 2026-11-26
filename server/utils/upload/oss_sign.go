package upload

import (
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// SignRemoteURL 为阿里云 OSS 对象签发带时效的临时直链。
//
// 公共读桶的裸链一旦被浏览器 Network 面板、代理日志或前端埋点记录下来就可永久转发,
// 会让业务侧 5 分钟 HMAC 短链的时效控制形同虚设; 302 目标改为签名链接后, 直链与短链同生命周期。
//
// disposition 非空时借 OSS 的 response 覆盖参数保住"另存为 + 原始文件名"——必须走 SDK option
// 参与签名计算, 手工把 response-content-disposition 拼到已签名的 URL 后面会因子资源不匹配返回 403。
//
// 返回空串表示当前存储后端不具备签名能力(非 aliyun-oss), 调用方应回退裸链。
func SignRemoteURL(key string, ttl time.Duration, disposition string) (string, error) {
	if !strings.EqualFold(global.GVA_CONFIG.System.OssType, "aliyun-oss") || key == "" {
		return "", nil
	}
	bucket, err := NewBucket()
	if err != nil {
		return "", err
	}
	var opts []oss.Option
	if disposition != "" {
		opts = append(opts, oss.ResponseContentDisposition(disposition))
	}
	sec := int64(ttl / time.Second)
	if sec < 1 {
		// 短链临近过期时也要签出一个可用窗口, 避免 302 后立刻 403
		sec = 1
	}
	return bucket.SignURL(key, oss.HTTPGet, sec, opts...)
}
