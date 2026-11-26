package middleware

import (
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/flipped-aurora/gin-vue-admin/server/config"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/gin-gonic/gin"
)

// Cors 直接放行所有跨域请求并放行所有 OPTIONS 方法
func Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		origin := c.Request.Header.Get("Origin")
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Headers", "Content-Type,AccessToken,X-CSRF-Token, Authorization, Token,X-Token,X-User-Id")
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS,DELETE,PUT")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type, New-Token, New-Expires-At")
		c.Header("Access-Control-Allow-Credentials", "true")

		// 放行所有OPTIONS方法
		if method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
		}
		// 处理请求
		c.Next()
	}
}

// CorsByRules 按照配置处理跨域请求
func CorsByRules() gin.HandlerFunc {
	// 放行全部
	if global.GVA_CONFIG.Cors.Mode == "allow-all" {
		return Cors()
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		// 无 Origin 头: 同源请求或非浏览器调用(curl/服务间/e2e 脚本), 不参与 CORS 判定直接放行,
		// 避免严格白名单模式把无 Origin 的正常请求一并 403
		if origin == "" {
			c.Next()
			return
		}
		whitelist := checkCors(origin)

		// 通过检查, 添加请求头(回显真实 origin, 兼容通配符配置与 Allow-Credentials)
		if whitelist != nil {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Headers", whitelist.AllowHeaders)
			c.Header("Access-Control-Allow-Methods", whitelist.AllowMethods)
			c.Header("Access-Control-Expose-Headers", whitelist.ExposeHeaders)
			if whitelist.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		// 严格白名单模式且未通过检查，直接拒绝处理请求
		if whitelist == nil && global.GVA_CONFIG.Cors.Mode == "strict-whitelist" && !(c.Request.Method == "GET" && c.Request.URL.Path == "/health") {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		// 预检请求统一在此短路(带上方已设置的 CORS 头), 不再进入业务/鉴权中间件
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		// 处理请求
		c.Next()
	}
}

var (
	corsPatternMu    sync.RWMutex
	corsPatternCache = map[string]*regexp.Regexp{}
)

// matchOrigin 判断实际 origin 是否命中配置项: 先精确匹配, 含 * 的按通配符(如 https://*.example.com)转正则匹配
func matchOrigin(pattern, origin string) bool {
	if pattern == "" {
		return false
	}
	if pattern == origin {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return false
	}
	corsPatternMu.RLock()
	re, ok := corsPatternCache[pattern]
	corsPatternMu.RUnlock()
	if !ok {
		var b strings.Builder
		b.WriteString("^")
		for i, seg := range strings.Split(pattern, "*") {
			if i > 0 {
				b.WriteString(".*")
			}
			b.WriteString(regexp.QuoteMeta(seg))
		}
		b.WriteString("$")
		re = regexp.MustCompile(b.String())
		corsPatternMu.Lock()
		corsPatternCache[pattern] = re
		corsPatternMu.Unlock()
	}
	return re.MatchString(origin)
}

func checkCors(currentOrigin string) *config.CORSWhitelist {
	for i := range global.GVA_CONFIG.Cors.Whitelist {
		// 支持精确匹配与通配符(*), 命中即返回
		if matchOrigin(global.GVA_CONFIG.Cors.Whitelist[i].AllowOrigin, currentOrigin) {
			return &global.GVA_CONFIG.Cors.Whitelist[i]
		}
	}
	return nil
}
