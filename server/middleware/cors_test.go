package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/gin-gonic/gin"
)

func TestMatchOrigin(t *testing.T) {
	cases := []struct {
		pattern, origin string
		want            bool
	}{
		{"https://kfapi.laigoushangcheng.cn", "https://kfapi.laigoushangcheng.cn", true},       // 精确
		{"http://*.laigoushangcheng.cn", "http://kfadmin663dc5e5.laigoushangcheng.cn", true},   // 通配符
		{"https://*.laigoushangcheng.cn", "http://kfadmin663dc5e5.laigoushangcheng.cn", false}, // 协议不符
		{"http://*.laigoushangcheng.cn", "http://evil.com", false},                             // 域名不符
		{"http://*.laigoushangcheng.cn", "http://laigoushangcheng.cn.evil.com", false},         // 后缀伪装
		{"https://a.example.com", "https://b.example.com", false},                              // 无通配符精确不等
	}
	for _, c := range cases {
		if got := matchOrigin(c.pattern, c.origin); got != c.want {
			t.Errorf("matchOrigin(%q,%q)=%v 期望 %v", c.pattern, c.origin, got, c.want)
		}
	}
}

func setupCorsRouter() *gin.Engine {
	global.GVA_CONFIG.Cors = config.CORS{
		Mode: "strict-whitelist",
		Whitelist: []config.CORSWhitelist{
			{
				AllowOrigin:      "http://*.laigoushangcheng.cn",
				AllowHeaders:     "Content-Type,X-Token",
				AllowMethods:     "POST, GET, OPTIONS",
				ExposeHeaders:    "Content-Length",
				AllowCredentials: true,
			},
		},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CorsByRules())
	r.POST("/base/captcha", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

// 预检: 白名单内动态子域应 204 且回显真实 Origin
func TestCorsPreflightAllowed(t *testing.T) {
	r := setupCorsRouter()
	req := httptest.NewRequest(http.MethodOptions, "/base/captcha", nil)
	req.Header.Set("Origin", "http://kfadmin663dc5e5.laigoushangcheng.cn")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("预检应 204, 实际 %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://kfadmin663dc5e5.laigoushangcheng.cn" {
		t.Fatalf("应回显真实 Origin, 实际 %q", got)
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("Allow-Credentials 应为 true")
	}
}

// 非白名单源: 预检应 403 且不带 Allow-Origin
func TestCorsPreflightDenied(t *testing.T) {
	r := setupCorsRouter()
	req := httptest.NewRequest(http.MethodOptions, "/base/captcha", nil)
	req.Header.Set("Origin", "http://evil.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("非白名单预检应 403, 实际 %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("拒绝时不应下发 Allow-Origin")
	}
}

// 无 Origin(服务端/curl/e2e): 应正常放行不被 403
func TestCorsNoOriginPassthrough(t *testing.T) {
	r := setupCorsRouter()
	req := httptest.NewRequest(http.MethodPost, "/base/captcha", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("无 Origin 应直达处理器, 实际 code=%d body=%q", w.Code, w.Body.String())
	}
}
