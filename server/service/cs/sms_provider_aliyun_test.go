package cs

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"go.uber.org/zap"
)

func init() {
	// Send 成功路径会写日志, 单测需保证 logger 非空
	if global.GVA_LOG == nil {
		global.GVA_LOG = zap.NewNop()
	}
}

func fixedProvider(endpoint string) *AliyunSmsProvider {
	return &AliyunSmsProvider{
		endpoint:         endpoint,
		region:           "cn-hangzhou",
		accessKey:        "test-ak",
		secretKey:        "test-sk",
		signName:         "杭州维铂叁网络科技",
		templateCode:     "SMS_337486476",
		templateParamKey: "code",
		client:           &http.Client{Timeout: 5 * time.Second},
		nowFunc:          func() time.Time { return time.Date(2026, 11, 26, 10, 30, 0, 0, time.UTC) },
		nonceFunc:        func() string { return "fixed-nonce-001" },
	}
}

func TestPercentEncodeAliyun(t *testing.T) {
	cases := map[string]string{
		"2016-02-23T22:52:04Z": "2016-02-23T22%3A52%3A04Z", // 冒号必须转义
		"a b":                  "a%20b",                    // 空格是 %20 而非 +
		"a*b":                  "a%2Ab",                    // 星号必须转义
		"a~b":                  "a~b",                      // 波浪号不转义
		"/":                    "%2F",
	}
	for in, want := range cases {
		if got := percentEncode(in); got != want {
			t.Errorf("percentEncode(%q)=%q 期望 %q", in, got, want)
		}
	}
	// 中文签名按 UTF-8 转义
	if got := percentEncode("杭州"); !strings.HasPrefix(got, "%E6%9D%AD") {
		t.Errorf("中文签名转义异常: %q", got)
	}
}

// 服务端按官方算法独立重算签名, 校验实现自洽(顺序/排除 Signature/编码后签名)
func TestAliyunSendSignsCanonicalSortedQuery(t *testing.T) {
	var gotQuery url.Values
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		gotQuery = r.URL.Query()
		if gotQuery.Get("Action") != "SendSms" || gotQuery.Get("Version") != aliyunSMSVersion {
			t.Errorf("Action/Version 不符: %s/%s", gotQuery.Get("Action"), gotQuery.Get("Version"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":"OK","RequestId":"req-1","BizId":"biz-1"}`))
	}))
	defer srv.Close()

	if err := fixedProvider(srv.URL).Send(context.Background(), "13800001111", "824137"); err != nil {
		t.Fatalf("期望发送成功, 实际 %v", err)
	}

	// 必填参数齐备且取值正确
	for k, want := range map[string]string{
		"PhoneNumbers": "13800001111", "SignName": "杭州维铂叁网络科技",
		"TemplateCode": "SMS_337486476", "TemplateParam": `{"code":"824137"}`,
		"AccessKeyId": "test-ak", "SignatureMethod": "HMAC-SHA1",
		"SignatureVersion": "1.0", "Format": "JSON", "RegionId": "cn-hangzhou",
		"SignatureNonce": "fixed-nonce-001", "Timestamp": "2026-11-26T10:30:00Z",
	} {
		if got := gotQuery.Get(k); got != want {
			t.Errorf("参数 %s=%q 期望 %q", k, got, want)
		}
	}

	// 参与签名的参数必须按 key 字节序排列, Signature 按惯例追加在末尾
	keys := strings.Split(rawQuery, "&")
	seen := make([]string, 0, len(keys))
	for _, kv := range keys {
		if k := strings.SplitN(kv, "=", 2)[0]; k != "Signature" {
			seen = append(seen, k)
		}
	}
	if !sort.StringsAreSorted(seen) {
		t.Errorf("签名参数未按 key 排序: %v", seen)
	}
	if idx := strings.Index(rawQuery, "&Signature="); idx < 0 {
		t.Error("缺少 Signature 参数")
	} else if strings.Contains(rawQuery[idx+1:], "&") {
		t.Error("Signature 应作为最后一个参数追加")
	}

	// 独立重算: 剔除 Signature 后重新规范编码并签名
	cp := make(map[string]string)
	for k := range gotQuery {
		if k != "Signature" {
			cp[k] = gotQuery.Get(k)
		}
	}
	names := make([]string, 0, len(cp))
	for k := range cp {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, percentEncode(k)+"="+percentEncode(cp[k]))
	}
	canonical := strings.Join(parts, "&")
	stringToSign := http.MethodGet + "&" + percentEncode("/") + "&" + percentEncode(canonical)
	mac := hmac.New(sha1.New, []byte("test-sk&"))
	mac.Write([]byte(stringToSign))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got := gotQuery.Get("Signature"); got != want {
		t.Errorf("签名不可复现: got=%q want=%q", got, want)
	}
	// 原始 URL 不得泄露 SK
	if strings.Contains(rawQuery, "test-sk") {
		t.Error("请求 URL 泄露了 AccessKeySecret")
	}
}

func TestAliyunSendPropagatesRejectionCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Code":"isv.SMS_SIGNATURE_ILLEGAL","Message":"InvalidApi signature not match","RequestId":"req-2"}`))
	}))
	defer srv.Close()

	err := fixedProvider(srv.URL).Send(context.Background(), "13800001111", "123456")
	if err == nil {
		t.Fatal("非 OK 响应应返回错误")
	}
	if !strings.Contains(err.Error(), "isv.SMS_SIGNATURE_ILLEGAL") {
		t.Errorf("错误应保留阿里云 Code 便于定位拒因, 实际: %v", err)
	}
	if strings.Contains(err.Error(), "test-sk") {
		t.Error("错误信息泄露 AccessKeySecret")
	}
}

func TestAliyunSendBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>502 Bad Gateway</html>`))
	}))
	defer srv.Close()
	if err := fixedProvider(srv.URL).Send(context.Background(), "13800001111", "123456"); err == nil {
		t.Fatal("非 JSON 响应应报错而不是静默成功")
	}
}

func TestGetSmsSenderAliyunValidation(t *testing.T) {
	restore := func() { smsClientOnce = nil; smsClientOnceErr = nil }
	restore() // 清除其他用例可能留下的单例
	defer restore()

	os.Unsetenv("GVA_SMS_AK")
	os.Unsetenv("GVA_SMS_SK")
	global.GVA_CONFIG.Sms = config.Sms{Provider: "aliyun", AccessKeyEnv: "GVA_SMS_AK", SecretKeyEnv: "GVA_SMS_SK", SignName: "s", TemplateCode: "SMS_1"}
	if _, err := GetSmsSender(); err == nil {
		t.Error("缺 AK/SK 时应构建失败")
	}
	restore()

	global.GVA_CONFIG.Sms = config.Sms{Provider: "aliyun", AccessKeyEnv: "GVA_SMS_AK", SecretKeyEnv: "GVA_SMS_SK"}
	os.Setenv("GVA_SMS_AK", "a")
	os.Setenv("GVA_SMS_SK", "b")
	defer func() { os.Unsetenv("GVA_SMS_AK"); os.Unsetenv("GVA_SMS_SK") }()
	if _, err := GetSmsSender(); err == nil {
		t.Error("缺签名/模板时应构建失败")
	}
	restore()

	global.GVA_CONFIG.Sms = config.Sms{Provider: "aliyun", AccessKeyEnv: "GVA_SMS_AK", SecretKeyEnv: "GVA_SMS_SK", SignName: "s", TemplateCode: "SMS_1"}
	sender, err := GetSmsSender()
	if err != nil {
		t.Fatalf("配置完整时应构建成功: %v", err)
	}
	p, ok := sender.(*AliyunSmsProvider)
	if !ok {
		t.Fatalf("类型应为 *AliyunSmsProvider, 实际 %T", sender)
	}
	if p.endpoint != aliyunSMSEndpointDefault || p.region != aliyunSMSRegionDefault || p.templateParamKey != "code" {
		t.Errorf("默认值未生效: endpoint=%q region=%q paramKey=%q", p.endpoint, p.region, p.templateParamKey)
	}
}
