# 客服系统 · 开发使用手册

> 适用版本：B2B 多商户架构落地后（e2e 66 项全绿基线）。更新于 2026-11。

## 1. 系统概览

```
┌────────────┐  ┌────────────┐  ┌──────────────┐
│ web(平台后台)│  │merchant-web │  │  chat-web    │
│ :8099      │  │ :8091      │  │ :8090 双入口  │
│ csPlatform │  │ 商户租户后台 │  │ h5.html 访客  │
└─────┬──────┘  └─────┬──────┘  │ workbench 客服│
      │               │         └───┬──────┬───┘
      └───────┬───────┘             │      │(WS)
              │ HTTP /api           │      ▼
              ▼                     ▼  ┌────────────┐
        ┌───────────┐  ┌────────┐  ┌──────────────┐
        │ gva-server│→ │ Kafka  │→ │storage-worker│─┐
        │  :8888    │  └────────┘  └──────────────┘ │
        └─┬───┬─────┘        ▲                      ▼
     pgsql redis mongo ──────┴────────── 消息落库(Mongo/pgsql)
                 ┌────────────┐
                 │ ws-gateway │ :8889  按会话广播推送
                 └────────────┘
```

四端登录签发**独立 Audience 的 cstoken**（`GVA-CS-<scope>`，scope ∈ platform/merchant/agent/member），互不通用；业务私有路由从 PublicGroup 派生、只挂 CsAuth 中间件，**不复用框架 PrivateGroup**（其 JWTAuth+Casbin 双重鉴权会导致业务令牌必然 403）。

## 2. 入口与演示账号

| 端 | 地址 | 账号 | 登录方式 |
|---|---|---|---|
| 平台后台·客服系统页 | `http://127.0.0.1:8099/#/csPlatform`（或侧边栏「客服系统→商户(租户)管理」）| 先 admin/123456 进框架，再页内 MFA：`platform` 域 | 图形码+短信码（dev 自动回填）|
| 商户后台（租户A）| `http://127.0.0.1:8091` | `merchantadmin/123456` | 图形码+短信码 |
| 商户后台（租户B）| 同上 | `shopbadmin/123456` | 同上 |
| 客服工作台 | `:8090/workbench.html[?t=<租户码>]` | `agent001/123456`（租户A）`shopb_agent/123456`（租户B）| 图形码+短信码 |
| 访客 H5 | `:8090/h5.html[?t=<租户码>]` | `member001/123456`（A）`shopb_member/123456`（B）| 仅账密 |

开发环境的图形验证码/短信码由服务端直接回显（响应中的 `devCaptcha`/`devCode`），前端自动回填，无需人工识别。

## 3. B2B 多渠道接入（重要）

- 接入链接 `?t=<租户code>`（亦接受 `tenant`/`code` 参数名）决定进入哪个商户；前端持久化于 localStorage（`cs_h5_tenant` / `cs_agent_tenant`）。
- 登录请求携带 `tenantCode`，服务端校验**账号归属租户与渠道标识一致**，错渠道报"不属于当前商户渠道"；商户被停用则访客/客服均拒绝登录，前端禁用登录按钮。
- 公开接口 `GET /member/tenant?code=<码>`（限 30/min/IP）返回商户名/状态/欢迎语，供接入页展示品牌。
- 平台侧「商户(租户)管理」对商户做全生命周期：开通（可同时建首个商户管理员）→ 编辑 → 停用/启用 → 注销删除（有未结束会话拒绝；软删租户+级联账号，审计保留）。
- 账号（客服/访客/商户管理员）`username` **全局唯一**（软删行仍占唯一索引），租户由账号归属隐式确定。

## 4. API 速查（前缀即 scope）

### member（访客，:8888 根路径）
| 方法/路径 | 说明 |
|---|---|
| POST `/member/login` | 账密 + **必带 `tenantCode`**（无渠道标识拒登，B2B 强制；10/min/IP 限流）|
| POST `/member/sso-login` | SSO 免密进入 `{tenantCode, user}`：user 为 AES-256-GCM 密文（key=SHA256(环境变量 GVA_CS_SSO_KEY)，格式 base64(iv[12B]+密文+tag)，明文 JSON `{username/externalId/phone/nickName/ts}`），服务端解密后自动写入/复用本商户 cs_members（source=SSO，跨租户同名自动加商户码前缀防误连）并直接签发访客令牌（20/min/IP）；h5 链接 `?t=<code>&user=<密文>` 自动登录进会话 |
| GET `/member/tenant?code=` | 公开商户渠道查询（30/min/IP）|
| POST `/member/conversations` | 建会话或取回活跃会话（CreateOrResume）|
| POST `/member/conversations/:id/messages` | 发消息 `{clientMsgId,type,body}`，type ∈ text\|image（60/min/账号）|
| GET `/member/conversations/:id/messages?limit=` | 历史（7 天窗口标记）|
| POST `/member/conversations/:id/read` | 已读水位 `{lastReadSeq}` |
| POST `/member/conversations/:id/files` | 上传图片（安全扫描 clean 后才可发消息引用）|
| GET `/member/files/:id/url` | 短时效签名下载链接 |

### agent（客服工作台）
`/agent/captcha` → `/agent/sms-code` → `/agent/login` 三段 MFA（请求体均可带 `tenantCode`）；
`GET /agent/conversations/pending|mine`；`POST /agent/conversations/:id/claim|read`；`POST /:id/transfer {toAgentId,reason}`；`POST /:id/close`；`GET /:id`（详情含用户资料+跨会话接待汇总）；`GET /:id/events`；`GET /agent/agents/transferable`（同租户可转客服）；发消息/文件同 member（120/min/账号）。

### merchant（商户后台，token=merchant 域）
三段 MFA `/merchant/captcha|sms-code|login`；`GET/POST/PUT /merchant/admin/agents[/password|/status]`；`GET/POST/PUT /merchant/admin/members[/password]`（仅本租户，claims.TenantID 强制）；`GET/PUT /merchant/admin/channel`（接待渠道配置：welcomeTemplate/domainWhitelist 等）；`GET /merchant/admin/audits`；`GET /merchant/admin/report/overview`。

### platform（平台客服系统管理，前缀特殊：`/cs/platform/*`）
三段 MFA `/cs/platform/captcha|sms-code|login`；`GET/POST/PUT /cs/platform/admin/tenants`；`PUT /tenants/status {id,status:1|2}`；`PUT /tenants/delete {id}`；`GET /tenants` 分页；`GET /audits`（全量，自身落审计）；`GET /overview`。

## 5. WebSocket 协议（ws-gateway :8889）

- 连接：`ws://<host>:8889/ws?token=<cstoken>`——**握手阶段强校验 query token**，缺失/无效直接拒。
- 订阅会话：`{"action":"subscribe","conversationId":<id>,"lastSeq":<n>}`；断线重连后需重新 subscribe（createWs 封装自动重发）。
- 订阅租户通道（仅 agent）：`{"action":"subscribeTenant"}` → 回 `tenant_subscribed`；接收同租户的会话生命周期事件与其他会话新消息，用于待接入数/列表实时刷新与未读提醒。
- 推送帧：`type ∈ message|sync|subscribed|tenant_subscribed|conv_event|tenant_message`；`message/sync/tenant_message` 的 body 为 MessageEvent；`conv_event` 的 body 为 `{tenantId,conversationId,op:created|claimed|transferred|closed,status,agentId,agentEmployeeNo,memberId,ts}`。
- 事件链路：会话 Create/Claim/Transfer/Close 后服务端向 Kafka **`im.conversation.event`**（配置 `kafka.event-topic`，尽力投递、失败不阻断业务）发事件；网关(cg-dispatch)同时消费 `im.message` 与该 topic，conv_event 广播给会话订阅者+租户客服通道。tenant_message 定向规则：**仅访客(senderType=member)消息触发**（客服自己/bot 消息不提醒）；MessageEvent 携带发送时刻的 `assigneeAgentId`（服务端取 conv.CurrentAgentID），已有当值客服时**只提醒当值者**，待接入(无当值)时提醒全租户客服鼓励认领；均只发给未订阅该会话的连接（避免双通道重复）。
- 心跳：30s 发 `{"action":"pong"}` 文本帧兜底；浏览器协议层 ping 由网关自动响应。
- 验收：`node scripts/e2e-notify.mjs`（10 项：租户通道订阅/created/claimed → h5 自动转已接入/tenant_message 未读源/closed）；`node scripts/e2e-targeted-notify.mjs`（6 项：有当值只提醒当值者/非当值不打扰/agent 消息不提醒/pending 提醒全租户/bot 不占提醒通道）。

## 6. 登录序列（自动化脚本用）

```
POST /<scope>/captcha            → data.devCaptcha, data.captchaId
POST /<scope>/sms-code {username,captcha,captchaId[,tenantCode]} → data.devCode
POST /<scope>/captcha            （图形码一次一用，login 前需重新获取）
POST /<scope>/login {username,password,captcha,captchaId,smsCode[,tenantCode]}
```
注意：短信有 **per-账号 60s 冷却**（`cs:sms:last:*`）与登录 **10/min/IP** 限流；连续测试前清 redis 键（见 §8）。

## 7. 前端工程结构

```
chat-web/          vite6 双入口: h5.html + workbench.html, src/common 公共层(api/ws/composer/ChatBubble)
merchant-web/      vite6 单入口, Element Plus
web/               rolldown-vite(vite8), csPlatform 页 = view/csPlatform/{index.vue,api.js}
                   菜单注入: pinia/modules/router.js SetAsyncRouter push CsPlatformGroup
```
三端 `/api` 代理均 rewrite 去前缀转发 `127.0.0.1:8888`。chat-web 已绑定 `0.0.0.0` 支持内网访问（`allowedHosts: true`），其余两端仅本机。

## 8. 测试与压测工具

| 工具 | 用法 |
|---|---|
| 全链路 e2e（66 项）| `bash scripts/e2e-cs.sh`（需 compose 全栈 + python3）|
| QPS 压测 | `node scripts/bench_cs.mjs [账号数] [每账号条数]`；`BENCH_RAW=1 BENCH_MAX_PER=200` 临时解除消息限流测基础容量 |
| 限流/冷却复位 | `docker exec gva-redis redis-cli --scan --pattern 'cs:rl:*' \| xargs -n20 docker exec gva-redis redis-cli DEL`；短信冷却键 `cs:sms:last:*` 同理 |
| bench 数据清理 | `docker exec gva-pgsql psql -U gva -d gva -c "DELETE FROM cs_conversations WHERE title='BENCH';"`（否则污染待接入列表断言）|

## 9. 开发约定（红线）

1. 一切查询/更新必须带 `tenant_id` 过滤（claims.TenantID 服务端强制，防 IDOR）；跨租户可见性以「会话成员」判定。
2. 业务私有路由只挂 `CsAuth`，从 PublicGroup 派生分组；绝不复用框架 PrivateGroup。
3. 新配置段需在 `server/config/config.go` 注册结构体字段才能被 viper 读取。
4. 手机号一律服务端按账号反查，拒绝客户端指定投递目标。
5. 消息类型白名单校验位于 `CanAccess` 之后、幂等 `SetNX` 之前（否则非法 type 留脏幂等键）。
6. AutoMigrate 与种子数据挂 `core/server.go` 的 `RunServer` 中 `Router := initialize.Routers()` 之前。
