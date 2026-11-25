# 客服系统 · 开发与部署指南

> 本机验证环境：macOS + Docker Desktop（compose 全栈 7 容器）。更新于 2026-11。

## 1. 依赖矩阵

| 组件 | 版本/镜像 | 宿主端口 | 容器名 |
|---|---|---|---|
| PostgreSQL | 17 | 15432→5432 | gva-pgsql（用户 gva / 库 gva）|
| Redis | 7 | 16379→6379 | gva-redis |
| MongoDB | 7 | 27018→27017 | gva-mongo |
| Kafka | apache/kafka 3.8.1（KRaft 单机）| 29092→9092 | gva-kafka |
| gin-vue-admin server | 本项目镜像 | 8888 | gva-server |
| ws-gateway | 同镜像不同入口 | 8889（0.0.0.0）| gva-ws-gateway |
| storage-worker | 同镜像 | 不对外 | gva-storage-worker |
| 前端 | Node ≥ 21（实测 26.x）| 8090/8091/8099 | — |

**共享开发机端口冲突是首要坑**：标准端口（5432/27017/9092 等）常被其他项目占用。端口映射冲突会导致容器"Up 但未加入网络"（`docker inspect` 的 `NetworkSettings.Networks` 为空 `{}`），同网容器访问报 `lookup <name> no such host`——遇到 no such host 先查网络成员和宿主端口占用（`lsof -nP -iTCP:<port> -sTCP:LISTEN`），不要盲目重启等待。

## 2. 后端全栈启动

```bash
cd <repo>
docker compose up -d          # 首次会拉镜像/建网络
docker ps --format '{{.Names}} {{.Status}}' | grep gva   # 7 个 Up
curl http://127.0.0.1:8888/health   # "ok"
```

- 服务端读取 `server/config.docker.yaml`（bind-mount **只读**覆盖镜像内文件，改配置免重建、重启容器即生效；但新增配置**段**必须先在该文件存在且已注册到 `config/config.go`）。
- 关键开关：`system.db-type: pgsql`、`use-redis: true`、`use-mongo: true`；`system.router-prefix` 保持空串（业务路由直接挂根路径）。
- `gva-kafka-init` 显示 Exited 属正常（一次性 topic 初始化任务）。
- 三个应用容器（server/ws-gateway/storage-worker）**共享同一镜像** `gva-server:pgsql`，仅入口参数不同。

### 数据库初始化（全新库时）
服务启动即连库，`GVA_DB != nil` 会让 `/init/initdb` 拒绝执行。标准流程：
1. 停服务；`docker exec gva-pgsql psql -U gva -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='gva'; DROP DATABASE gva;"`
2. 临时置空 `config.docker.yaml` 的 `db-name` → `docker compose up -d server`
3. `POST /init/initdb`（dbType=pgsql、host=gva-pgsql、dbName=gva、adminPassword=123456）
4. 恢复 `db-name: gva` 并 `docker compose restart server`；`GET /init/checkdb` 应 `needInit:false`
> initdb 末尾可能报 `open config.docker.yaml: read-only file system`——那是 :ro 挂载下 WriteConfig 失败，**种子数据在此之前已全部落库**，勿据此重跑。

## 3. 前端启动（开发模式）

```bash
cd chat-web      && npm install && npm run dev   # :8090 双入口, 已绑 0.0.0.0 支持内网
cd merchant-web  && npm install && npm run dev   # :8091
cd web           && npm install && npm run dev -- --port 8099 --strictPort   # 平台后台(web 默认 8080)
```

- 三端 `/api` 代理 rewrite 去前缀 → `http://127.0.0.1:8888`；WS 直连 `:8889`（拼接 `location.hostname`，内网/远程访问自动跟随）。
- 内网访问 chat-web 的配置点：`vite.config.js` `server.host:'0.0.0.0'` + `allowedHosts:true`，且 **package.json dev 脚本的 `--host` 会覆盖配置文件**，两处都要改。
- 构建：各端 `npm run build` 产物独立 dist（web 用 rolldown-vite `build.rolldownOptions.input`，chat-web/merchant-web 用 vite6 `build.rollupOptions.input`）。

## 4. 代码变更生效路径

| 改动 | 生效方式 |
|---|---|
| server 任意 Go 代码（含 ws-gateway/worker 入口）| `docker compose build server && docker compose up -d --force-recreate server ws-gateway storage-worker`（镜像共享，三个都要重建）|
| config.docker.yaml | `docker compose restart server`（:ro 挂载改文件即生效）|
| 前端 dev | vite 热更新即时生效；改 vite.config/package.json 需重启 dev |
| Redis 限流/冷却状态 | `docker exec gva-redis redis-cli --scan --pattern 'cs:rl:*' \| xargs -n20 docker exec gva-redis redis-cli DEL`；短信冷却 `cs:sms:last:*` 同理 |

## 5. 验证与回归

```bash
bash scripts/e2e-cs.sh                 # 66 项全链路（含 B2B 多商户口径）
node scripts/bench_cs.mjs 30 55        # 消息链路快速压测
```
e2e 前置：compose 全栈 + 演示账号存在（镜像内含幂等种子）。**压测后务必清理 BENCH 会话**，否则待接入列表断言被脏数据冲掉：
```sql
DELETE FROM cs_conversation_members WHERE conversation_id IN (SELECT id FROM cs_conversations WHERE title='BENCH');
DELETE FROM cs_read_cursors        WHERE conversation_id IN (SELECT id FROM cs_conversations WHERE title='BENCH');
DELETE FROM cs_assignment_events   WHERE conversation_id IN (SELECT id FROM cs_conversations WHERE title='BENCH');
DELETE FROM cs_conversations WHERE title='BENCH';
```

## 6. 生产部署配置清单

### 6.1 配置文件四层结构

| 层 | 文件 | 说明 |
|---|---|---|
| **敏感配置单一来源** | **根目录 `.env`**（模板 `.env.example`，已 gitignore） | 所有密钥/口令集中于此，`docker compose` 启动时自动加载；生产 `cp .env.example .env` 填真实值 |
| 后端主配置 | **`server/config.docker.yaml`** | 容器实际生效入口（compose `:ro` bind-mount 覆盖镜像内同名文件）；非敏感项在此，敏感项可被 `.env` 覆盖（见 6.2）；改完 `docker compose restart server` 即生效 |
| 环境变量注入 | 根 `docker-compose.yml` 的 `x-app-env` 锚点 | server/storage-worker/ws-gateway 三进程共享同一套 `${GVA_*}` 插值；pgsql/redis/mongo 容器凭据与程序用同源变量，不会改出不一致 |
| 前端构建变量 | `web/.env.production`、`chat-web/.env.production`、`merchant-web/.env.production` | 生产为静态产物，改配置 = 重新 `npm run build`；`VITE_BASE_API=/api` 配合 nginx 反代 |

**env 覆盖机制**（`server/core/env_override.go`，三进程均经 `core.Viper()` 生效）：配置加载后按下列环境变量**非空则覆盖** yaml 同名字段，未设置完全保持 yaml 原值（向后兼容）：

| 环境变量 | 覆盖目标 |
|---|---|
| `GVA_JWT_SIGNING_KEY` | `jwt.signing-key`（换钥=全员重登，建议部署最后一步） |
| `GVA_DB_USERNAME/PASSWORD/NAME` | `system.db-type` 所选主库（pgsql/mysql/...）的用户/密码/库名，与 pgsql 容器 `POSTGRES_*` 同源 |
| `GVA_REDIS_PASSWORD` | `redis.password`；非空时 compose 的 redis 容器同时自动开启 `requirepass` |
| `GVA_MONGO_USERNAME/PASSWORD` | `mongo` 认证；两者均非空时 mongo 容器启用 root 鉴权（**仅对全新数据卷生效**，存量卷需手动 `createUser`+`setFeatureCompatibilityVersion` 开 auth） |
| `GVA_OSS_ACCESS_KEY_ID/SECRET` | 按 `system.oss-type` 覆盖对应 OSS 段（aliyun/minio/qiniu/tencent/aws/cloudflare/huawei 全支持；local 无凭据） |
| `GVA_CS_SSO_KEY` | SSO AES 密钥（程序 `os.Getenv` 原生读取，非覆盖机制） |
| `GVA_SMS_AK/SK` | 短信凭证（同上，env 名由 `sms.access-key-env/secret-key-env` 指定） |

> 注意：改 `.env` 后 `docker compose up -d` 会自动重建 env 变化的容器；覆盖机制不支持 `db-list` 多库段与 `local` 存储路径（仍在 yaml 配）。

⚠️ `deploy/docker-compose/`、`deploy/kubernetes/` 是 gin-vue-admin 原版遗留样例（mysql、`env: develop`），**与本项目实际部署的根目录 `docker-compose.yml` 无关**，勿改错文件；若要上 k8s 需按根 compose 的服务拓扑整体重建编排。

### 6.2 逐项必改（`config.docker.yaml` 内）

**① JWT 签名密钥（L4-8）——最高优先级**
```yaml
jwt:
    signing-key: qmPlus     # ← 框架默认值，生产必须换强随机串
```
推荐不在 yaml 填真实值，改用根目录 `.env` 的 `GVA_JWT_SIGNING_KEY` 覆盖（见 6.1 机制表）。所有令牌域（框架 + cstoken）与文件下载 HMAC 短链均由它派生；更换后旧令牌全部失效（全员重登）。

**② 短信（L115-127）**
```yaml
sms:
    provider: log           # → http（通用HTTP网关）
    endpoint: ""            # → 短信网关地址
    sign-name / template-code   # → 短信签名与模板 Code
    dev-return-code: true   # → 必须 false！否则验证码直接回显，等同 MFA 旁路
```
AK/SK 不落文件：`access-key-env: GVA_SMS_AK`、`secret-key-env: GVA_SMS_SK` 指定**环境变量名**，密钥值由部署环境注入。

**③ SSO AES 密钥（L103-105）**
```yaml
sso:
    aes-key-env: GVA_CS_SSO_KEY   # 文件里只有变量名；未注入=接口自动拒绝服务
    max-age-sec: 0                # 生产建议开启时效（如 300 秒）
```
⚠️ 密钥真实值已集中到根目录 `.env` 的 `GVA_CS_SSO_KEY`（开发值 `gva-cs-sso-dev-key-2026` 不入库；compose 仅插值不再写死明文），生产替换 `.env` 值后 `docker compose up -d` 重建生效。商户侧对接规范见 [sso-php.md](sso-php.md)。

**④ OSS（L89 + L205-282）**
- 选型开关：`system.oss-type: local` → `aliyun-oss` / `qiniu` / `minio` / `tencent-cos` / `aws-s3` / `cloudflare-r2` / `hua-wei-obs`
- AK/SK 推荐走 `.env` 的 `GVA_OSS_ACCESS_KEY_ID/GVA_OSS_ACCESS_KEY_SECRET`（按 oss-type 自动落到对应段，yaml 只留 endpoint/bucket 等非敏感项）；若仍写在 yaml，**切勿把真实凭证提交进 git**
- `local` 模式存储目录 `local.path: uploads/file`（compose 已用具名卷 `gva-server-uploads` 持久化）

**⑤ 数据库/中间件凭据（现均已在 `.env` 集中，yaml 里的开发值仅作未覆盖时的兑底）**
```env
# 根目录 .env
GVA_DB_USERNAME=gva / GVA_DB_PASSWORD=gva123 / GVA_DB_NAME=gva   # → 生产强密码(与 pgsql 容器同源)
GVA_REDIS_PASSWORD=          # → 非空即启用 redis 容器 requirepass + 程序同步生效
GVA_MONGO_USERNAME= / GVA_MONGO_PASSWORD=   # → 非空即启用 mongo root 鉴权(仅全新卷生效)
```
compose 的 pgsql `POSTGRES_USER/PASSWORD/DB`、redis `requirepass`、mongo `MONGO_INITDB_ROOT_*` 与上表是**同一组变量**，改 `.env` 一处即全栈同步。kafka brokers 仍在 yaml（非敏感）；`db-list` 多库段不支持 env 覆盖。

**⑥ 系统开关（L85-100）**
- `system.env: local` → `public`（关闭路由日志输出）
- `use-redis/use-mongo: true` 不能关（客服链路强依赖）；`db-type: pgsql` 保持
- `system.router-prefix` 若 nginx 要加前缀在此改，**前端三端 API 路径与 e2e 脚本需同步**
- `captcha.open-captcha`、`iplimit-*` 按业务复核

### 6.3 敏感凭证现状与处置速查

| 凭证 | 现在在哪 | 生产动作 |
|---|---|---|
| JWT signing-key | yaml L5 默认 `qmPlus`；可被 `.env` `GVA_JWT_SIGNING_KEY` 覆盖 | `.env` 填强随机 |
| 短信 AK/SK | `.env` `GVA_SMS_AK/GVA_SMS_SK`（log 模式未用到） | 部署环境注入 |
| SSO AES 密钥 | `.env` `GVA_CS_SSO_KEY`（开发值；compose 不再写死明文） | `.env` 换生产密钥 |
| pgsql 密码 | `.env` `GVA_DB_PASSWORD`（与 pgsql 容器同源） | `.env` 改强密码一处生效 |
| OSS AK/SK | `.env` `GVA_OSS_ACCESS_KEY_ID/SECRET`（按 oss-type 落段） | `.env` 填真实值 |
| admin 初始密码 | 种子/initdb `123456` | 首登立即改 |

安全红线全集另见 [security-ratelimit.md](security-ratelimit.md) §2.4（生产必查开关清单）。

### 6.4 其他部署要点

- 前端 `csPlatform` 接入链接的 `:8090` dev 端口写死（`web/src/view/csPlatform/index.vue` 的 `chatBase`）需替换为正式域名。
- 限流值按业务 SLA 复核（访客发消息 60/min/账号、客服 120/min、登录 10/min/IP、渠道查询 30/min/IP）。
- HTTPS 部署时 WS 地址自动切 `wss://`（`chat-web/src/common/api.js` 按 `location.protocol` 判断），需在网关前置反代开启 WebSocket upgrade 透传。
- 容量参考（见测试报告）：单实例 gva-server 消息链路吞吐 ~1000-1800 msg/s，瓶颈依次为业务限流 → server CPU → Kafka 单 broker → pgsql；扩容优先 server 水平复制 + Kafka 分区数。

## 7. 常见问题速查

| 症状 | 根因/处置 |
|---|---|
| 容器 Up 但 `lookup gva-xxx no such host` | 宿主端口冲突致未入网：docker inspect Networks 为 `{}` → 改 ports 映射 + force-recreate |
| 登录报"发送过于频繁" | 短信 60s per-账号冷却，清 `cs:sms:last:*` |
| 登录报"请求过于频繁" | 登录 10/min/IP 限流（容器内看到的 IP 是 docker 网关如 192.168.32.1），清 `cs:rl:*login*` |
| 业务接口 401"无效令牌" | 用了别端 token（令牌域隔离是特性不是 bug）或误走框架 JWTAuth 路由 |
| 脚本收到 "404 page not found" 文本 | 平台端前缀是 `/cs/platform/*` 而非 `/platform/*`；先探 http_code 再解析 JSON |
| 8889 根路径 HTTP 404 | 正常，ws-gateway 只支持 `/ws` upgrade 握手 |
| ws 连接立即失败 | URL 必须带 `?token=`，网关握手阶段强校验 |
| mongo 连不上但服务照常起 | mongo 初始化失败不致命，需 grep 启动日志"链接mongodb数据库失败"确认 |
