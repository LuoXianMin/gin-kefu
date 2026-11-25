# 客服系统安全与限流报告

> 生成日期：2026-11-25 · 适用版本：main 分支（e2e-cs.sh 67 项全绿基线）
> 范围：访客 H5 / 客服工作台 / 商户后台 / 平台后台四端 + gva-server / ws-gateway / storage-worker 三个服务进程。所有结论以当前代码与实测为准，未实现项明确列入"已知边界"。

---

## 1. 结论摘要

| 维度 | 现状 | 评级 |
|---|---|---|
| 认证强度 | 三端 MFA（账密+图形码+短信）、bcrypt、独立令牌域互不通用 | 强 |
| 租户隔离/授权 | 服务端 claims 强制租户上下文，会话级成员授权，IDOR 实测拒绝 | 强 |
| 文件安全 | 扩展名白名单 + 10MB 上限 + sha256 判重 + 扫描 + HMAC 5 分钟短链 | 强（扫描器为开发版特征） |
| 接口限流 | Redis 固定窗口，登录/短信/消息/审计分接口分级，IP 与账号双维度 | 中（fail-open、窗口边界可突发） |
| 防暴力破解 | 账号失败 5 次锁 15 分钟（三端 MFA 域）+ 短信码连错 5 次作废 | 中（**访客端无账号级锁定**，见 §8） |
| 敏感信息保护 | 手机号全链路脱敏、验证码回显受配置开关控制 | 强（生产须关闭 dev 开关） |

---

## 2. 认证与令牌安全

### 2.1 四端独立令牌域
- 令牌由 `utils/cstoken` 签发：HS256，Claims 含 `scope/accountId/tenantId/username/employeeNo`，Audience 固定 `GVA-CS-<scope>`（`server/utils/cstoken/jwt.go`）。
- `middleware.CsAuth(scope)` 三重校验：令牌有效 → **scope 与 Audience 双一致**（跨端令牌一律拒绝，`ErrScopeMismatch`）→ 黑名单（复用框架 `BlackCache`，登出即拉黑）。
- 客服业务域不复用框架 PrivateGroup（避免 JWTAuth+Casbin 双重鉴权），从 PublicGroup 派生分组只挂 CsAuth——授权边界由 scope+租户上下文承担。

### 2.2 登录 MFA 链路（platform/merchant/agent 三端）
- 序列：图形验证码（base64Captcha 内存 store，答案不落任何接口）→ 短信验证码 → 账密。
- **手机号由服务端按 scope+账号反查**，拒绝客户端任意指定（`AuthService.resolvePhone`），防把 MFA 短信导向他人号码。
- 校验顺序即安全设计：AgentLogin 的渠道（tenantCode）校验置于短信消费**之前**，错渠道拒绝时验证码不被消耗，同时拒绝跨商户账号串入（e2e [14] 实测"不属于当前商户渠道"）。
- 访客端（member）仅账密（产品设计如此，无手机号绑定要求），但 **tenantCode 渠道标识必填**：无渠道直接拒登，接入链接（h5.html?t=＜code＞）是唯一登录入口。

### 2.3 口令与令牌生命周期
- 口令 bcrypt 散列存储（`CheckPassword/HashPassword`），全链路无明文。
- 令牌有效期 `jwt.expires-time: 7d`（配置值，签发时写入 exp），无 refresh 机制（见 §8）。
- 平台后台免二次登录：`POST /cs/platform/exchange` 仅接受**框架 JWTAuth 已登录且 authorityId=888 超管**，换取的 cs 平台令牌与 MFA 登录同域同权限——不降低安全水位，仅省去重复认证。实测：非超管拒绝、无框架登录态 401（2026-11-24）。

### 2.4 开发后门开关（生产必查清单）
| 开关 | 当前值 | 生产要求 |
|---|---|---|
| `sms.dev-return-code` | `true`（接口返回 `devCode`） | **必须 false**，否则短信验证码直接回显 |
| 图形码 `devCaptcha` 自动回填 | 开发环境返回 | 随部署配置关闭 |
| `sms.provider: log` | 验证码写日志 | 切换真实短信网关（http provider + AK/SK 环境变量） |

---

## 3. 授权与租户隔离（防 IDOR）

- **租户上下文只信令牌**：`CsTenantScope` 从 claims 注入 `tenantID`；客户端显式携带不一致的 `?tenantId=` 直接按越权拒绝（"跨租户访问被拒绝"）。
- **会话级授权**：`ConversationService.CanAccess(tenantID, scope, accountID, convID)` 同时校验会话同租户 + 请求者是 `cs_conversation_members` 成员；REST 发消息、历史、详情、WS subscribe 全部走此门。实测（e2e [4]/[9]）：非成员访问他人会话、不存在会话均被拒（IDOR 防护断言）。
- **WS 订阅不信任客户端会话 ID**：`handleSubscribe` 服务端 CanAccess 后才登记连接；`subscribeTenant` 仅 agent scope 可用。
- **操作归属校验**：close/transfer 校验 `CurrentAgentID` 当值者（ErrNotOwner）；访客消息引用附件校验 `UploaderType+UploaderID` 双一致——防止借用他人已扫描附件（e2e 实测"仿用他人附件被拒"）。
- **商户/平台管理面**：商户接口一律以 claims.TenantID 强制本租户范围；账号 username 全局唯一索引，杜绝跨租户重名劫持语义。
- 消息类型白名单：仅 `text|image`（e2e 实测 `type=video` 被拒），文本体 ≤8192 字节，image 体必须是含 fileId 的合法 JSON。

---

## 4. 文件安全

| 环节 | 机制 |
|---|---|
| 上传准入 | 会话成员 + 会话未结束（CanAccess）→ 扩展名白名单（图片/文档/压缩包/文本 13 种）→ 10MB 上限 |
| 判重 | sha256 全会话比对命中即拒（ErrUploadDuplicate），防同源反复灌库 |
| 扫描 | 上传后同步扫描（开发版：EICAR 特征 + 空文件判违规），状态 `pending/clean/infected` 落库 |
| 发链 | `IssueDownloadURL`：infected 永不发链、未扫描完成不发链（避免下载层窗口期绕过） |
| 下载 | 公开端点 `/cs/files/dl?fid&exp&sig`：HMAC-SHA256 签名（密钥为 JWT signing-key，消息域 `cs-file:{id}:{exp}` 隔离用途），`subtle.ConstantTimeCompare` 恒时比较防时序攻击，5 分钟时效，下载层再次拒绝 infected |

改进项：扫描器接真实引擎（ClamAV 等）后当前结构即插即用（§8-4）。

---

## 5. 限流策略总表

实现：`middleware.CsRateLimit(limit, window)`——Redis `INCR+EXPIRE` 固定窗口；key = `cs:rl:<路由>:<身份>`，**已登录按 `scope:accountID`，匿名按客户端 IP**；超限返回业务码（msg"请求过于频繁"）并计入 `MetricRateLimited` 指标（`/cs/metrics` 可观测）。

| 接口 | 限额 | 维度 |
|---|---|---|
| POST /member/login | 10/min | IP |
| POST /{agent,merchant,cs/platform}/login | 20/min | IP（登录前无身份） |
| POST /{agent,merchant,cs/platform}/sms-code | 10/min | IP |
| GET /member/tenant | 30/min | IP |
| POST /{member}/conversations/:id/messages | **60/min** | 账号 |
| POST /agent/conversations/:id/messages | **120/min** | 账号 |
| GET /merchant/admin/audits、/cs/platform/admin/audits | 30/min | 账号 |

**Redis 不可用时 fail-open 放行**：限流定位是护栏而非授权边界（授权由 CsAuth 独立保证），避免缓存故障放大为全站不可用。

压测佐证（docs/cs/test-report.md）：默认限流下业务限流是第一天花板——单账号 ~60 条后即被拒；解除后消息链路稳定 ~1086 QPS（30 账号 1650 条 1.52s，0 失败）。

## 6. 防暴力破解与验证码防护

- **账号失败锁定**（三端 MFA 域）：登录失败 `cs:login:fail:<scope>:<username>` 计数，首次失败起 **15 分钟**窗口内累计 **5 次即锁定**，成功登录清零。实测（2026-11-25）：第 6/7 次尝试返回"账号已锁定，请稍后重试"，键 TTL=900s。
- **短信码防爆破**：每码校验失败计数（`sms:fail` 10 分钟窗口），连续错 `max-fail-count: 5` 次该码作废需重发；校验通过即删码（**一次性消费**）。
- **短信轰炸防护**：发送前须通过图形验证码；同一账号 **60s 冷却**（实测二次发送被"发送过于频繁"拒绝）+ **每日 10 条上限**；手机号服务端反查（§2.2）限制发送接口被用作骚扰他人通道。
- **图形验证码**：4 位数字，内存 store，一次一答。
- **幂等防重放**：消息 clientMsgId 幂等键 24h——重复/恶意重发同键请求返回原结果（`deduplicated:true`），不产生新消息、不消耗 seq。

## 7. 传输与运行时防护

- WS 网关：连接必须携带有效 cs 令牌（member/agent scope 之外 403）；`SetReadLimit 8KB` 防帧灌弹；60s 无 pong 主动断开；发送通道带缓冲，写满丢弃该帧（客户端可补拉，不阻塞广播循环）。
- 会话内 seq 由 Redis Lua 原子分配（INCR 基线兜底），Redis 故障**拒绝发送**（fail-closed，与限流相反取向：一致性边界不允许 degraded 写入）。
- Kafka `acks=all` 同步确认后才向客户端 ACK 消息成功；事件/提醒/欢迎语等旁路链路尽力投递、失败仅日志。
- WS 补拉与历史查询均以服务端授权为前提（backfill 仅在 subscribe 鉴权通过后触发），不存在"猜到会话 ID 即可拉历史"的路径。

## 8. 已知边界与改进建议

| # | 现状 | 风险 | 建议 |
|---|---|---|---|
| 1 | **访客端(member)登录无账号级失败锁定**，仅 IP 限流 10/min（实测 2026-11-25：同一不存在账号连续错密 6 次仍为"账号或密码错误"，不锁定） | 分布式低频撞库可绕过 IP 维度 | MemberLogin 接入与三端相同的 `checkLock/recordFail`（键已按 scope 隔离，改动小） |
| 2 | 限流 fail-open + 固定窗口 | Redis 故障期限流失效（授权仍安全）；窗口交界理论可 2×limit 突发 | 需要更平滑时换滑动窗口/令牌桶（redis 已具备原子条件） |
| 3 | 令牌 7d 无服务端吊销通道（除登出黑名单，黑名单为进程内缓存） | 令牌泄露后长有效期 | 缩短 cs 域过期或接入 Redis 全局吊销列表 |
| 4 | 文件扫描为开发版特征（EICAR） | 真实恶意样本可能漏检 | 生产接 ClamAV/对象存储扫描服务，接口已按状态机预留 |
| 5 | `dev-return-code/devCaptcha` 当前开启 | **等同 MFA 旁路** | 上线检查清单硬卡（§2.4） |
| 6 | 登录失败/限流拒绝暂未写审计流水（审计目前覆盖账号/租户管理操作） | 攻击面可观测性不足 | audits 增加认证失败事件类型 |
| 7 | 渠道域名白名单 `domainWhitelist` 字段已存储，服务端未对 Origin 强制校验 | 配置项承诺未闭环 | CsTenantScope 增加 Origin 匹配或在接入层网关执行 |

## 9. 实测记录汇总（本轮）

| 项 | 结果 |
|---|---|
| IP 登录限流 | 第 11 次/分钟被拒："请求过于频繁, 请稍后再试" ✅ |
| 账号失败锁定（agent 域） | 第 6 次起"账号已锁定，请稍后重试"，TTL 900s ✅ |
| 访客域失败锁定 | 不触发（列入 §8-1）⚠️ |
| 短信 60s 冷却 | 二次发送"发送过于频繁" ✅ |
| exchange 越权面 | 非超管拒绝 / 无框架登录态 401 ✅（2026-11-24） |
| IDOR / 附件仿用 / 类型白名单 / 跨商户渠道 | e2e-cs.sh 对应断言全绿（67/67）✅ |

> 复现方式：`bash scripts/e2e-cs.sh`；限流/锁定 live 用例参见本报告 §5/§6 描述键位（`cs:rl:*`、`cs:login:fail:*`、`cs:sms:last:*`），测试后清理这些 Redis 键即可恢复环境。
