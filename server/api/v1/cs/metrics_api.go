package cs

import (
	"net/http"

	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/gin-gonic/gin"
)

// MetricsApi Prometheus 文本指标端点(客服域)
type MetricsApi struct{}

func (a *MetricsApi) Exposure(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.String(http.StatusOK, cssvc.RenderMetrics(c.Request.Context()))
}
