package middleware

import (
	"context"
	"strconv"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/flipped-aurora/gin-vue-admin/server/model/common/response"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"github.com/gin-gonic/gin"
)

// CsRateLimit Redis 固定窗口限流。key = 路由 + 身份(已登录按 scope:accountID, 匿名按 IP)。
// Redis 不可用时放行(fail-open): 限流是护栏而非授权边界, 授权由 CsAuth 保证。
func CsRateLimit(limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if global.GVA_REDIS == nil {
			c.Next()
			return
		}
		who := c.ClientIP()
		if v, exists := c.Get(CsClaimsKey); exists {
			if claims, ok := v.(*cstoken.CsClaims); ok && claims != nil {
				who = claims.Scope + ":" + strconv.FormatUint(uint64(claims.AccountID), 10)
			}
		}
		key := "cs:rl:" + c.FullPath() + ":" + who
		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()
		n, err := global.GVA_REDIS.Incr(ctx, key).Result()
		if err != nil {
			c.Next()
			return
		}
		if n == 1 {
			global.GVA_REDIS.Expire(ctx, key, window)
		}
		if n > int64(limit) {
			cssvc.MetricIncr(ctx, cssvc.MetricRateLimited, c.FullPath())
			response.FailWithMessage("请求过于频繁, 请稍后再试", c)
			c.Abort()
			return
		}
		c.Next()
	}
}
