package upload

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"go.uber.org/zap"
)

// 1x1 透明 PNG, 供真实上传探针使用
const livePNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// liveOssConfig 从环境变量自举 OSS 配置(单测进程不加载 yaml)。
// endpoint/bucket 等非敏感项沿用仓库 config.docker.yaml 的现值, AK/SK 必须由环境提供。
func liveOssConfig() config.AliyunOSS {
	pick := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	cfg := config.AliyunOSS{
		Endpoint:        pick("GVA_OSS_ENDPOINT", "https://oss-cn-beijing.aliyuncs.com"),
		BucketName:      pick("GVA_OSS_BUCKET", "huashop"),
		BucketUrl:       pick("GVA_OSS_BUCKET_URL", "https://huashop.oss-cn-beijing.aliyuncs.com"),
		BasePath:        pick("GVA_OSS_BASE_PATH", "kefu"),
		AccessKeyId:     os.Getenv("GVA_OSS_ACCESS_KEY_ID"),
		AccessKeySecret: os.Getenv("GVA_OSS_ACCESS_KEY_SECRET"),
	}
	return cfg
}

// newLiveFileHeader 把内存图片包成 multipart.FileHeader, 模拟前端上传。
// 显式写 part 的 Content-Type: 业务层靠 file.Header.Get("Content-Type") 落 MimeType,
// 而 MimeType 又决定下载层走图片直链还是附件 disposition, 不可省略。
func newLiveFileHeader(t *testing.T, name, contentType string, data []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, name))
	hdr.Set("Content-Type", contentType)
	part, err := w.CreatePart(hdr)
	if err != nil {
		t.Fatalf("构造上传表单失败: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("写入图片内容失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("解析 multipart 表单失败: %v", err)
	}
	return req.MultipartForm.File["file"][0]
}

// TestAliyunOssLiveUploadRoundTrip 真实验证 OSS 通道: 上传 -> 公网读回比对 -> 删除 -> 确认消失。
// 默认 skip, 仅 GVA_OSS_LIVE=1 时执行; 对象名带时间戳且用完即删, 不在 bucket 留残留。
func TestAliyunOssLiveUploadRoundTrip(t *testing.T) {
	if os.Getenv("GVA_OSS_LIVE") != "1" {
		t.Skip("需显式设置 GVA_OSS_LIVE=1 才会真实写入 OSS bucket")
	}
	ossCfg := liveOssConfig()
	if ossCfg.AccessKeyId == "" || ossCfg.AccessKeySecret == "" {
		t.Fatal("缺少 GVA_OSS_ACCESS_KEY_ID / GVA_OSS_ACCESS_KEY_SECRET")
	}
	global.GVA_LOG = zap.NewNop()
	global.GVA_CONFIG.System.OssType = "aliyun-oss"
	global.GVA_CONFIG.AliyunOSS = ossCfg

	raw, err := base64.StdEncoding.DecodeString(livePNGBase64)
	if err != nil {
		t.Fatalf("测试图片解码失败: %v", err)
	}
	name := "gva-probe-" + time.Now().UTC().Format("20060102150405") + ".png"
	fh := newLiveFileHeader(t, name, "image/png", raw)

	o := NewOss()
	if _, ok := o.(*AliyunOSS); !ok {
		t.Fatalf("NewOss() 未分派到 AliyunOSS 而是 %T, 说明 oss-type 取值不匹配(注意 default 会静默回落 local)", o)
	}

	fileURL, key, err := o.UploadFile(fh)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	t.Logf("已上传: key=%s url=%s", key, fileURL)
	defer func() {
		if derr := o.DeleteFile(key); derr != nil {
			t.Errorf("清理测试对象失败, 需手工删除 bucket 上的 %s: %v", key, derr)
		}
	}()

	if !strings.HasPrefix(fileURL, ossCfg.BucketUrl+"/") {
		t.Errorf("返回 URL 前缀异常: %s", fileURL)
	}
	if !strings.Contains(key, name) {
		t.Errorf("object key 未包含存储名: %s", key)
	}

	// 公共读校验: 不带任何凭证直接 GET 必须 200 且内容一致, 否则前端 <img> 无法渲染
	resp, err := http.Get(fileURL)
	if err != nil {
		t.Fatalf("公网读取失败(桶可能非公共读或域名不可达): %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("公网读取状态码 %d, 桶可能不是公共读: %s", resp.StatusCode, string(body))
	}
	if !bytes.Equal(body, raw) {
		t.Errorf("读回内容与上传内容不一致: got %d bytes, want %d", len(body), len(raw))
	}

	// 签名直链必须真实可用: SDK 会把 object key 里的 / 编码为 %2F,
	// 只有实测 200 才能确认 OSS 解码回原对象; 否则下载层 302 到签名链会全面裂图。
	signed, err := SignRemoteURL(key, 60*time.Second, "")
	if err != nil {
		t.Fatalf("签发签名直链失败: %v", err)
	}
	if !strings.Contains(signed, "Signature=") {
		t.Fatalf("签名直链缺少 Signature: %s", signed)
	}
	sresp, err := http.Get(signed)
	if err != nil {
		t.Fatalf("签名直链请求失败: %v", err)
	}
	sbody, _ := io.ReadAll(io.LimitReader(sresp.Body, 1<<20))
	_ = sresp.Body.Close()
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("签名直链不可访问(%d): %s —— 302 到它会导致图片加载失败", sresp.StatusCode, string(sbody))
	}
	if !bytes.Equal(sbody, raw) {
		t.Errorf("签名直链内容与上传内容不一致")
	}
	t.Logf("签名直链可用: %s", signed)

	// response-content-disposition 参与签名计算, 编码不一致会返 403 SignatureDoesNotMatch,
	// 必须实测 OSS 不仅接受而且回显该头。
	disp := "attachment; filename*=UTF-8''" + url.PathEscape("探针 图片.png")
	signedDisp, err := SignRemoteURL(key, 60*time.Second, disp)
	if err != nil {
		t.Fatalf("签发带 disposition 的签名直链失败: %v", err)
	}
	dresp, err := http.Get(signedDisp)
	if err != nil {
		t.Fatalf("带 disposition 签名直链请求失败: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(dresp.Body, 1<<20))
	_ = dresp.Body.Close()
	if dresp.StatusCode != http.StatusOK {
		t.Fatalf("带 disposition 签名直链不可访问(%d), 可能是子资源未参与签名", dresp.StatusCode)
	}
	t.Logf("OSS 回显 Content-Disposition: %q", dresp.Header.Get("Content-Disposition"))

	// 删除必须真实生效, 否则存储清理接口是空转
	if err := o.DeleteFile(key); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	gone, err := http.Get(fileURL)
	if err != nil {
		t.Fatalf("删除后探测请求失败: %v", err)
	}
	_ = gone.Body.Close()
	if gone.StatusCode == http.StatusOK {
		t.Errorf("删除后仍可访问 200, DeleteFile 未生效")
	} else {
		t.Logf("删除后状态码 %d(预期 403/404)", gone.StatusCode)
	}
}
