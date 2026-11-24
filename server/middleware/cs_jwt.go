package middleware

import (
	"strconv"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	csmodel "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"github.com/gin-gonic/gin"
)

const CsClaimsKey = "csClaims"

// CsAuth 各端独立鉴权: 校验令牌有效 + Audience 与所在端一致 + 未命中黑名单
func CsAuth(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := cstoken.GetToken(c)
		if token == "" {
			response.NoAuth("未登录或非法访问，请登录", c)
			c.Abort()
			return
		}
		if isCsBlacklist(token) {
			response.NoAuth("令牌已失效，请重新登录", c)
			c.Abort()
			return
		}
		claims, err := cstoken.ParseToken(token)
		if err != nil {
			if err == cstoken.ErrTokenExpired {
				response.NoAuth("登录已过期，请重新登录", c)
			} else {
				response.NoAuth("无效令牌", c)
			}
			c.Abort()
			return
		}
		// 跨端令牌一律拒绝(平台token不能访问商户端，反之亦然)
		if claims.Scope != scope || !cstoken.AudienceMatches(claims, scope) {
			response.NoAuth(cstoken.ErrScopeMismatch.Error(), c)
			c.Abort()
			return
		}
		c.Set(CsClaimsKey, claims)
		c.Next()
	}
}

// CsTenantScope 租户上下文: 只信令牌不信客户端; platform 端跳过
func CsTenantScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if scope == csmodel.ScopePlatform {
			c.Next()
			return
		}
		claims, exists := c.Get(CsClaimsKey)
		if !exists {
			response.NoAuth("缺少租户上下文", c)
			c.Abort()
			return
		}
		cs := claims.(*cstoken.CsClaims)
		if cs.TenantID == 0 {
			response.NoAuth("非法账号: 无租户归属", c)
			c.Abort()
			return
		}
		// 客户端显式携带 tenantId 且与令牌不一致 → 按越权拒绝并留审计(D期接入 audit_logs)
		if clientTenant := c.Query("tenantId"); clientTenant != "" && clientTenant != strconv.FormatUint(uint64(cs.TenantID), 10) {
			response.NoAuth("跨租户访问被拒绝", c)
			c.Abort()
			return
		}
		c.Set("tenantID", cs.TenantID)
		c.Next()
	}
}

// isCsBlacklist 与框架 JWT 黑名单机制一致(BlackCache)，退出登录时写入
func isCsBlacklist(token string) bool {
	_, ok := global.BlackCache.Get(token)
	return ok
}
