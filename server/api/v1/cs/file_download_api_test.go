package cs

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	csModel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
)

func TestIsRemoteURL(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"https://huashop.oss-cn-beijing.aliyuncs.com/kefu/uploads/a.png", true},
		{"http://127.0.0.1:9000/bucket/a.png", true},
		{"uploads/file/a.png", false},
		{"/go/src/server/uploads/file/a.png", false},
		{"", false},
		{"httpsx//not-a-scheme", false},
	}
	for _, c := range cases {
		if got := isRemoteURL(c.path); got != c.want {
			t.Errorf("isRemoteURL(%q) = %v, 期望 %v", c.path, got, c.want)
		}
	}
}

// useAliyunOSS 把存储后端切到 aliyun-oss 并注入测试凭证。
// SignURL 是纯本地签名计算, 不发网络请求, 因此可安全在单测中走这条分支。
func useAliyunOSS(t *testing.T) {
	t.Helper()
	oldType := global.GVA_CONFIG.System.OssType
	oldOSS := global.GVA_CONFIG.AliyunOSS
	t.Cleanup(func() {
		global.GVA_CONFIG.System.OssType = oldType
		global.GVA_CONFIG.AliyunOSS = oldOSS
	})
	global.GVA_CONFIG.System.OssType = "aliyun-oss"
	global.GVA_CONFIG.AliyunOSS = config.AliyunOSS{
		Endpoint:        "https://oss-cn-beijing.aliyuncs.com",
		AccessKeyId:     "test-ak",
		AccessKeySecret: "test-sk",
		BucketName:      "huashop",
		BucketUrl:       "https://huashop.oss-cn-beijing.aliyuncs.com",
		BasePath:        "kefu",
	}
}

func sampleImage() *csModel.CsFileAttachment {
	return &csModel.CsFileAttachment{
		FilePath:   "https://huashop.oss-cn-beijing.aliyuncs.com/kefu/uploads/2026-11-26/abc.png",
		StoredName: "kefu/uploads/2026-11-26/abc.png",
		MimeType:   "image/png",
		FileName:   "截图 1.png",
	}
}

// 未配置 aliyun-oss 时(如 minio/其他后端)无签名能力, 回退裸链与 query 覆盖
func TestRemoteDownloadTargetImageUsesBareURL(t *testing.T) {
	f := sampleImage()
	got := remoteDownloadTarget(f, time.Now().Add(300*time.Second).Unix())
	if got != f.FilePath {
		t.Fatalf("无签名能力时图片应返回裸链接, 实际: %s", got)
	}
	if strings.Contains(got, "response-content-disposition") {
		t.Errorf("图片不应带 disposition 覆盖参数: %s", got)
	}
}

func TestRemoteDownloadTargetAttachmentCarriesDisposition(t *testing.T) {
	f := sampleImage()
	f.FilePath = "https://huashop.oss-cn-beijing.aliyuncs.com/kefu/uploads/report.pdf"
	f.StoredName = "kefu/uploads/report.pdf"
	f.MimeType = "application/pdf"
	f.FileName = "季度报告 v2.pdf"

	got := remoteDownloadTarget(f, time.Now().Add(300*time.Second).Unix())
	base := f.FilePath + "?response-content-disposition="
	if !strings.HasPrefix(got, base) {
		t.Fatalf("缺少 disposition 覆盖参数, 实际: %s", got)
	}

	// query 值解码一次后必须是合法的头值, 且文件名部分按 RFC 5987 编码
	raw := strings.TrimPrefix(got, base)
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		t.Fatalf("disposition 参数编码非法: %v", err)
	}
	if !strings.HasPrefix(decoded, "attachment; filename*=UTF-8''") {
		t.Errorf("头值格式错误: %s", decoded)
	}
	name := strings.TrimPrefix(decoded, "attachment; filename*=UTF-8''")
	if name == "" || name == f.FileName {
		t.Errorf("文件名应经 percent-encode, 实际: %q", name)
	}
	back, err := url.PathUnescape(name)
	if err != nil || back != f.FileName {
		t.Errorf("文件名解码后应还原为原始名, 得到 %q (err=%v)", back, err)
	}
	if strings.Contains(raw, " ") {
		t.Errorf("query 值中的空格必须被编码: %s", raw)
	}
}

// MimeType 缺失(老数据或未写 Content-Type)时按附件处理, 保守保住下载语义而非直接渲染
func TestRemoteDownloadTargetUnknownMimeFallsBackToAttachment(t *testing.T) {
	f := sampleImage()
	f.FilePath = "https://huashop.oss-cn-beijing.aliyuncs.com/kefu/uploads/x.bin"
	f.StoredName = "kefu/uploads/x.bin"
	f.MimeType = ""
	f.FileName = "x.bin"
	if !strings.Contains(remoteDownloadTarget(f, time.Now().Add(60*time.Second).Unix()), "response-content-disposition") {
		t.Errorf("未知类型应回退为附件下载")
	}
}

// 核心安全语义: aliyun-oss 下 302 目标必须是带时效的签名链, 不能是永久有效的公共读裸链
func TestRemoteDownloadTargetPrefersSignedURLForAliyun(t *testing.T) {
	useAliyunOSS(t)
	f := sampleImage()

	got := remoteDownloadTarget(f, time.Now().Add(300*time.Second).Unix())
	if got == f.FilePath {
		t.Fatalf("仍返回公共读裸链, 时效控制被绕过: %s", got)
	}
	for _, want := range []string{"Signature=", "OSSAccessKeyId=test-ak", "Expires="} {
		if !strings.Contains(got, want) {
			t.Errorf("签名链缺少 %s: %s", want, got)
		}
	}
	// SDK 会把 object key 里的 / 编码为 %2F(OSS 侧解码回原值), 故只校验 bucket 前缀
	if !strings.HasPrefix(got, "https://huashop.oss-cn-beijing.aliyuncs.com/") {
		t.Errorf("签名链 bucket 前缀异常: %s", got)
	}
	// 图片不附 disposition, 以便命中 OSS/CDN 缓存
	if strings.Contains(got, "response-content-disposition") {
		t.Errorf("图片签名链不应带 disposition: %s", got)
	}
}

// 附件的 disposition 必须参与签名(SDK option), 否则 OSS 会因子资源不匹配拒绝
func TestRemoteDownloadTargetSignedAttachmentSignsDisposition(t *testing.T) {
	useAliyunOSS(t)
	f := sampleImage()
	f.FilePath = "https://huashop.oss-cn-beijing.aliyuncs.com/kefu/uploads/report.pdf"
	f.StoredName = "kefu/uploads/report.pdf"
	f.MimeType = "application/pdf"
	f.FileName = "月报.pdf"

	got := remoteDownloadTarget(f, time.Now().Add(300*time.Second).Unix())
	if !strings.Contains(got, "Signature=") {
		t.Fatalf("附件应走签名链: %s", got)
	}
	if !strings.Contains(got, "response-content-disposition") {
		t.Errorf("附件签名链应含 disposition 子资源: %s", got)
	}
	// disposition 必须排在签名参数之前参与计算: 由 SDK 统一生成, 此处校验未被手工追加
	q, err := url.Parse(got)
	if err != nil {
		t.Fatalf("签名链不是合法 URL: %v", err)
	}
	if q.Query().Get("response-content-disposition") == "" {
		t.Errorf("disposition 参数值为空: %s", got)
	}
}

// 签名时长不得超过短链剩余寿命, 否则 302 目标会比授权活得更久
func TestRemainingTTL(t *testing.T) {
	now := time.Now().Unix()
	if d := remainingTTL(now + 300); d < 295*time.Second || d > 300*time.Second {
		t.Errorf("剩余 300s 应得 ~300s, 实际 %v", d)
	}
	// 已过期(理论上 ResolveDownload 会先拒)也要给出正数窗口, 避免签出 0 秒链
	if d := remainingTTL(now - 10); d <= 0 {
		t.Errorf("过期 exp 应回退为正数窗口, 实际 %v", d)
	}
}
