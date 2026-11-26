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

> 运维视角的完整上线步骤（含初始化/nginx/核对清单/回滚）见 [production-deploy.md](production-deploy.md)，本节聚焦配置分布与逐项必改说明。

### 6.1 配置文件四层结构

| 层 | 文件 | 说明 |
|---|---|---|
| **敏感配置单一来源** | **根目录 `.env`**（模板 `.env.example`，已 gitignore） | 所有密钥/口令集中于此，`docker compose` 启动时自动加载；生产 `cp .env.example .env` 填真实值 |
| 后端主配置 | **`server/config.docker.yaml`** | 容器实际生效入口（compose `:ro` bind-mount 覆盖镜像内同名文件）；非敏感项在此，敏感项可被 `.env` 覆盖（见 6.2）；改完 `docker compose restart server` 即生效 |
| 环境变量注入 | 根 `docker-compose.yml` 的 `x-app-env` 锚点 | server/storage-worker/ws-gateway 三进程共享同一套 `${GVA_*}` 插值；pgsql/redis/mongo 容器凭据与程序用同源变量，不会改出不一致 |
| 前端构建变量 | `web/.env.production`、`chat-web/.env.production`、`merchant-web/.env.production` | 生产为静态产物，改配置 = 重新 `npm run build`；**直连模式**：API base 填后端绝对域名（`VITE_API_BASE`/`VITE_BASE_API`=`https://kfapi.laigoushangcheng.cn`，不带 `/api`），与后端同域同源不走反代 |

**env 覆盖机制**（`server/core/env_override.go`，三进程均经 `core.Viper()` 生效）：配置加载后按下列环境变量**非空则覆盖** yaml 同名字段，未设置完全保持 yaml 原值（向后兼容）：

| 环境变量 | 覆盖目标 |
|---|---|
| `GVA_JWT_SIGNING_KEY` | `jwt.signing-key`（换钥=全员重登，建议部署最后一步） |
| `GVA_DB_USERNAME/PASSWORD/NAME` | `system.db-type` 所选主库（pgsql/mysql/...）的用户/密码/库名，与 pgsql 容器 `POSTGRES_*` 同源 |
| `GVA_REDIS_PASSWORD` | `redis.password`；非空时 compose 的 redis 容器同时自动开启 `requirepass` |
| `GVA_MONGO_USERNAME/PASSWORD` | `mongo` 认证；两者均非空时 mongo 容器启用 root 鉴权（**仅对全新数据卷生效**，存量卷需手动 `createUser`+`setFeatureCompatibilityVersion` 开 auth） |
| `GVA_OSS_ACCESS_KEY_ID/SECRET` | 按 `system.oss-type` 覆盖对应 OSS 段（aliyun/minio/qiniu/tencent/aws/cloudflare/huawei 全支持；local 无凭据） |
| `GVA_SMS_PROVIDER/ENDPOINT/SIGN_NAME/TEMPLATE_CODE/TEMPLATE_PARAM_KEY/REGION` | `sms` 段的发送通道与模板配置（生产无需改 yaml，整段可在 `.env` 配齐） |
| `GVA_SMS_DEV_RETURN_CODE` | `sms.dev-return-code`（bool）；**生产必设 false**，留空用 yaml 值 |
| `GVA_SYSTEM_ENV` | `system.env`（`local`/`public`）；`public`=图形验证码不回显答案 |
| `GVA_DB_TYPE` / `GVA_OSS_TYPE` | `system.db-type` / `system.oss-type`（**先于**主库凭据与 OSS AK/SK 生效，自动落到新类型对应段） |
| `GVA_ROUTER_PREFIX` | `system.router-prefix`（改动需同步前端三端与 e2e 脚本） |
| `GVA_ZAP_LEVEL` | `zap.level`（`debug`/`info`/`warn`/`error`） |
| `GVA_DISABLE_AUTO_MIGRATE` | `system.disable-auto-migrate`（bool，生产改 true 走手动迁移） |
| `GVA_SSO_MAX_AGE_SEC` | `sso.max-age-sec`（int 秒，>0 校验 SSO 链接 `ts` 时效，防重放） |
| `GVA_OPEN_CAPTCHA` | `captcha.open-captcha`（int，0=每次登录都要图形码，>0=错 N 次后出码） |
| `GVA_KAFKA_ENABLED` / `GVA_KAFKA_BROKERS` | `kafka.enabled`（bool）/ `kafka.brokers`（逗号分隔列表） |
| `GIN_MODE` | gin 运行模式（`debug`/`release`，compose 直接注入 `${GIN_MODE:-debug}`，非覆盖机制） |
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

**② 短信（L115-129）**
整段已可在 `.env` 配置而不动 yaml（非空即覆盖同名 yaml 项）：
```env
GVA_SMS_PROVIDER=aliyun        # 开发 yaml 为 log(只写日志不真发); aliyun=真发阿里云短信
GVA_SMS_ENDPOINT=              # aliyun 留空即用 https://dysmsapi.aliyuncs.com; http 模式才必填
GVA_SMS_SIGN_NAME=<签名>
GVA_SMS_TEMPLATE_CODE=<模板Code>
GVA_SMS_TEMPLATE_PARAM_KEY=code # 必须与模板正文里的 ${xxx} 一致, 否则 isv.TEMPLATE_PARAMS_ILLEGAL
GVA_SMS_REGION=                # 留空=cn-hangzhou
GVA_SMS_DEV_RETURN_CODE=false  # 必须！留空则沿用 yaml 的 true，等同 MFA 旁路
GVA_SMS_AK= / GVA_SMS_SK=      # 阿里云 AccessKey（程序按 sms.access-key-env/secret-key-env 指定的变量名读）
```
yaml 内保留的 `provider: log`/`dev-return-code: true` 仅作开发兜底，生产以 `.env` 为准。

两个易踩点（已经代码核实）：
- **`provider=log` 时接口仍返回 `code:0 验证码已发送`**，但实际未发起任何外呼（仅 `LogProvider` 写一条 Info 日志）；若生产 `GVA_ZAP_LEVEL=warn`，连那条日志都看不到，极像“发送成功但丢短信”。
- **`provider=http` 不是阿里云**：它按自定义协议 POST `{phone,code,sign,tpl}` + `X-Access-Key/X-Timestamp/X-Signature`（HMAC-SHA256），接阿里云端点会直接签名失败；阿里云请用 `provider=aliyun`（OpenAPI RPC 签名 HMAC-SHA1，无 SDK 依赖）。
可用 `GVA_SMS_LIVE=1 go test ./service/cs/ -run TestAliyunLiveSignatureAccepted -v` 校验凭证与签名（用例传非法号格式，不计费也不会发出短信，返回 `isv.MOBILE_NUMBER_ILLEGAL` 即证明签名被阿里云接受）。

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

**客服附件（图片/文件）切 OSS 的三个耦合点**

1. 上传统一走 `service/cs/file_service.go` → `upload.NewOss()`，落哪里完全由 `system.oss-type` 决定，业务代码不感知。但 `NewOss()` 的 **`default` 分支静默回落 `Local`**（`utils/upload/upload.go:43`）——`oss-type` 写错（如误填 `aliyun`）不报错、不降级提示，图片继续存容器盘。切换后必须实测确认，不能只看配置。
2. 下载统一走 HMAC 短链 `/cs/files/dl`，服务端按 `CsFileAttachment.FilePath` 形态分流：本地路径 → `c.File()`；`http(s)://` → **302 重定向到带时效的 OSS 签名直链**（`upload.SignRemoteURL`，签名时长对齐短链剩余寿命 `exp`，不让 302 目标比授权活得更久）。图片不附 disposition 以命中 OSS/CDN 缓存，其余附件借 `oss.ResponseContentDisposition` option 保住“另存为 + 原始文件名”——**该参数必须走 SDK option 参与签名**，手工拼到已签名 URL 后面会因子资源不匹配返回 403。因此**切 OSS 无需数据迁移**，历史 local 记录照常可读，前端 `getFileUrl` 零改动。
3. 因为 302 目标是签名链，**桶不要求公共读，建议设为私有**（签名对私有对象同样有效，且可彻底消除“直链被转发则永久可访问”的隐私风险）。若桶为公共读，签名链与裸链都能访问；若改为非阿里云后端（如 minio）则不具备签名能力，会回退裸链 + query 形式的 `response-content-disposition`，此时才需要公共读。

object key 唯一性：阿里云实现用**原始文件名**拼 key（`utils/upload/aliyun_oss.go:31`），同名文件会互相覆盖导致历史消息串图；客服域已在调用前把 `file.Filename` 换为内容 sha256 前 24 位（同内容同 key 覆盖无害，已被秒传判重拦截），原始名留存于 `FileName` 字段供展示与下载。

校验（真实写﹢读回﹢删一个探针对象，默认 skip）：

```bash
cd server && set -a && . ../.env && set +a \
  && GVA_OSS_LIVE=1 go test ./utils/upload/ -run TestAliyunOssLiveUploadRoundTrip -v
```

命中“已上传 … url=https://…”、“签名直链可用”、“OSS 回显 Content-Disposition”与“删除后状态码 404”即证明凭证、endpoint、bucket、base-path 全部正确。其中签名直链一项尤其关键：SDK 会把 object key 里的 `/` 编码为 `%2F`，**只有实测 200 才能确认 OSS 解码回原对象**（否则 302 到签名链会全面裂图，而这一点在配置和代码里都看不出来）；用例还会断言 `NewOss()` 真的分派到 `AliyunOSS`（上面第 1 点的静默回落会被它拦住）。

**⑤ 数据库/中间件凭据（现均已在 `.env` 集中，yaml 里的开发值仅作未覆盖时的兜底）**
```env
# 根目录 .env
GVA_DB_USERNAME=gva / GVA_DB_PASSWORD=gva123 / GVA_DB_NAME=gva   # → 生产强密码(与 pgsql 容器同源)
GVA_REDIS_PASSWORD=          # → 非空即启用 redis 容器 requirepass + 程序同步生效
GVA_MONGO_USERNAME= / GVA_MONGO_PASSWORD=   # → 非空即启用 mongo root 鉴权(仅全新卷生效)
```
compose 的 pgsql `POSTGRES_USER/PASSWORD/DB`、redis `requirepass`、mongo `MONGO_INITDB_ROOT_*` 与上表是**同一组变量**，改 `.env` 一处即全栈同步（mongo 两项已去掉 compose 明文兜底：留空=无鉴权开发态，与程序/容器严格同源）。kafka brokers 可用 `GVA_KAFKA_BROKERS` 覆盖；`db-list` 多库段不支持 env 覆盖。⚠️ mongo 鉴权**仅对全新数据卷生效**——若在已有存量卷时设置过凭据后又想回退无鉴权开发态，需 `docker volume rm <project>_gva-mongo-data` 重建（卷内有 admin 用户则 mongod 强制鉴权，清空前注意消息数据）。

**⑥ 系统开关（L85-100）——已全部可经 `.env` 覆盖，生产无需改 yaml**
```env
GVA_SYSTEM_ENV=public        # 关闭图形码答案回显
GIN_MODE=release             # 关 gin 路由日志
GVA_ZAP_LEVEL=warn           # 生产日志级别
GVA_DISABLE_AUTO_MIGRATE=true# 生产建议手动迁移表结构
GVA_SSO_MAX_AGE_SEC=300      # SSO 链接时效防重放
GVA_OSS_TYPE=aliyun-oss      # 与 GVA_OSS_ACCESS_KEY_ID/SECRET 自动配套
```
- `use-redis/use-mongo: true` 不能关（客服链路强依赖）；`db-type: pgsql` 保持
- `system.router-prefix` 若 nginx 要加前缀改 `GVA_ROUTER_PREFIX`，**前端三端 API 路径与 e2e 脚本需同步**
- `captcha.open-captcha`（`GVA_OPEN_CAPTCHA`）、`iplimit-*` 按业务复核

### 6.3 敏感凭证现状与处置速查

| 凭证 | 现在在哪 | 生产动作 |
|---|---|---|
| JWT signing-key | yaml L5 默认 `qmPlus`；可被 `.env` `GVA_JWT_SIGNING_KEY` 覆盖 | `.env` 填强随机 |
| 短信 AK/SK 与模板配置 | `.env` `GVA_SMS_AK/SK/PROVIDER/ENDPOINT/SIGN_NAME/TEMPLATE_CODE/TEMPLATE_PARAM_KEY/REGION/DEV_RETURN_CODE` | 部署环境注入 |
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
| 接口回"验证码已发送"但手机没收到 | `sms.provider` 仍为 `log`（只写日志不真发），生产需 `GVA_SMS_PROVIDER=aliyun` 并重建/restart |
| 短信报 `isv.TEMPLATE_PARAMS_ILLEGAL` | `GVA_SMS_TEMPLATE_PARAM_KEY` 与阿里云模板正文变量名不一致（模板写 `${code}` 则填 `code`） |
| 登录报"发送过于频繁" | 短信 60s per-账号冷却，清 `cs:sms:last:*` |
| 登录报"请求过于频繁" | 登录 10/min/IP 限流（容器内看到的 IP 是 docker 网关如 192.168.32.1），清 `cs:rl:*login*` |
| 业务接口 401"无效令牌" | 用了别端 token（令牌域隔离是特性不是 bug）或误走框架 JWTAuth 路由 |
| 脚本收到 "404 page not found" 文本 | 平台端前缀是 `/cs/platform/*` 而非 `/platform/*`；先探 http_code 再解析 JSON |
| 8889 根路径 HTTP 404 | 正常，ws-gateway 只支持 `/ws` upgrade 握手 |
| ws 连接立即失败 | URL 必须带 `?token=`，网关握手阶段强校验 |
| 切了 `GVA_OSS_TYPE=aliyun-oss` 但图片仍存容器 `uploads/` | `oss-type` 取值拼错，被 `NewOss()` 的 `default` 静默回落 local（不报错）；正确值是 `aliyun-oss`，用 §6.2④ 的 live 用例验证 |
| 图片切 OSS 后前端裂图 | 先看 302 的 `Location` 返回值：`SignatureDoesNotMatch`=子资源未参与签名（必须走 `oss.ResponseContentDisposition` option）；`NoSuchKey`=key 拼接/base-path 错；`AccessDenied`=非 aliyun 后端回退了裸链而桶是私有 |
| `https://kfapi…/ws` 握手失败但网关 `/healthz` 200 | 边缘反代（1Panel/OpenResty）缺 `location /ws`，请求落到主服务返回 Gin 纯文本 404；转发后应返回 **401 unauthorized**（网关拒假 token）即代表链路已通 |
| mongo 连不上但服务照常起 | mongo 初始化失败不致命，需 grep 启动日志"链接mongodb数据库失败"确认 |
