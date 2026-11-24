# Security Policy

## Reporting a Vulnerability

Please report security issues to qimiaojiangjizhao@gmail.com
# 访客 h5 + 客服工作台 (8090, 已绑定 0.0.0.0 支持内网访问)
cd /Users/min/GolandProjects/gin-vue-admin-main/chat-web && npm run dev

# 商户后台 (8091)
cd /Users/min/GolandProjects/gin-vue-admin-main/merchant-web && npm run dev

# 平台后台 (8099, 需显式端口)
cd /Users/min/GolandProjects/gin-vue-admin-main/web && npm run dev -- --port 8099 --strictPort

启动后可访问：
访客端 http://127.0.0.1:8090/h5.html?t=shopa、客服端 http://127.0.0.1:8090/workbench.html?t=shopa（内网设备用 192.168.1.12:8090）
商户后台 http://127.0.0.1:8091，平台后台 http://127.0.0.1:8099