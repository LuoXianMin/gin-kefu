# 客服系统 · 生产部署手册

> 面向运维的一页式上线流程。配置机制详解见 [deployment.md](deployment.md) §6，安全红线见 [security-ratelimit.md](security-ratelimit.md)。
> 部署形态：根目录 `docker-compose.yml` 全栈 8 服务 + 三端静态产物由 nginx 托管。

## 0. 服务拓扑

| 容器 | 端口(宿主) | 说明 |
|---|---|---|
| gva-server | 8888 | 业务 API（server） |
| gva-ws-gateway | 8889 | WebSocket 推送（仅 `/ws?token=`） |
| gva-storage-worker | 不对外 | Kafka→Mongo 落库 + seq 水位 |
| gva-pgsql | 5432* | 主库（生产建议不映射宿主端口） |
| gva-redis | 6379* | 限流/验证码/seq |
| gva-mongo | 27017* | 消息存储 |
| gva-kafka | 9092* | 消息/事件双 topic |
| gva-kafka-init | — | 一次性建 topic，Exited 属正常 |

*中间件端口仅供容器内网互访（`gva-net`），生产应在 compose 中删除宿主 `ports` 映射，绝不暴露公网。

## 1. 前置条件

- Linux + Docker ≥ 24（compose v2）；单实例参考配置 4C8G
- 域名与证书：API 域（如 `api.example.com`）、WS 域（可同域，需 upgrade 透传）、三端静态站点
- 短信通道：阿里云短信已审核通过的**签名 + 模板 Code + AK/SK**（`provider=aliyun` 模式，无需 SDK）；若用自建短信网关则走 `provider=http`（约定 HMAC-SHA256 报文，非阿里云 OpenAPI）
- 对象存储桶：客服附件（图片/文件）生产走 `oss-type: aliyun-oss`。下载层 302 到**带时效的 OSS 签名直链**，因此**建议把桶设为私有**（消除“公共读直链被转发则永久可访问”的隐私风险）；非敏感项（endpoint/bucket-name/bucket-url/base-path）只能在 yaml 的 `aliyun-oss:` 段配，`.env` 仅覆盖 AK/SK

## 2. 集中配置：`.env`（唯一密钥入口）

```bash
git clone git@github.com:LuoXianMin/gin-kefu.git && cd gin-kefu
cp .env.example .env && chmod 600 .env
```

逐项填写 `.env`（消费方：compose 插值 + `server/core/env_override.go`，非空即覆盖 yaml）：

| 变量 | 生产取值 | 说明 |
|---|---|---|
| `GVA_JWT_SIGNING_KEY` | `openssl rand -hex 32` | **必设**。所有令牌域+文件短链 HMAC 根密钥；留空则沿用 yaml 默认 `qmPlus`（红线） |
| `GVA_CS_SSO_KEY` | `openssl rand -hex 16` | **启用 SSO 必设**。留空=SSO 接口自动禁用；下发给商户按 [sso-php.md](sso-php.md) 对接 |
| `GVA_DB_USERNAME/PASSWORD/NAME` | 强密码 | 与 pgsql 容器 `POSTGRES_*` 同源，一处生效全栈 |
| `GVA_REDIS_PASSWORD` | 强密码 | 非空即 redis 容器开 `requirepass` + 程序同步生效 |
| `GVA_MONGO_USERNAME/PASSWORD` | 强密码 | ⚠️ 仅对**全新数据卷**生效；存量卷需手动 `createUser`+ 开 auth |
| `GVA_SMS_PROVIDER` | **`aliyun`** | 覆盖 yaml 的 `log`（`log` 只写日志不真发，接口仍回 `code:0 验证码已发送`，容易误判为已发） |
| `GVA_SMS_ENDPOINT/SIGN_NAME/TEMPLATE_CODE` | 阿里云留空/签名/模板 | 短信环境配置整段无需再动 yaml；endpoint 留空即用 `https://dysmsapi.aliyuncs.com` |
| `GVA_SMS_TEMPLATE_PARAM_KEY` | 模板变量名（留空=`code`） | 必须与阿里云模板正文里的 `${xxx}` 一致，否则报 `isv.TEMPLATE_PARAMS_ILLEGAL` |
| `GVA_SMS_REGION` | 留空=`cn-hangzhou` | aliyun 短信地域 |
| `GVA_SMS_DEV_RETURN_CODE` | **`false`（必须显式）** | 留空沿用 yaml 开发值 `true`=验证码直接回显，MFA 形同虚设 |
| `GVA_SMS_AK/SK` | 阿里云 AccessKey | provider=aliyun/http 时必需（变量名由 `sms.access-key-env` 指定） |
| `GVA_OSS_ACCESS_KEY_ID/SECRET` | 桶凭证 | 按 `oss-type` 自动落到对应 OSS 段 |
| `GIN_MODE` | `release` | 关 gin 路由日志（开发留空 debug） |
| `GVA_SYSTEM_ENV` | `public` | 图形验证码不回显答案 |
| `GVA_ZAP_LEVEL` | `warn` | 生产日志级别 |
| `GVA_DISABLE_AUTO_MIGRATE` | `true`（若手动迁移） | 否则保持 AutoMigrate 自动建表 |
| `GVA_SSO_MAX_AGE_SEC` | `300` | SSO 链接时效防重放（0/留空=不校验） |
| `GVA_OSS_TYPE` / `GVA_DB_TYPE` | 按选型（OSS 生产填 `aliyun-oss`） | 非空覆盖 yaml 同名项，与 AK/SK、DB 凭据自动配套。❗ OSS 取值必须**精确等于** `aliyun-oss`，写错会被 `NewOss()` 的 `default` 静默回落 local（不报错，图片仍存容器盘） |
| `GVA_KAFKA_BROKERS` | 集群地址 | 逗号分隔；换生产 Kafka 无需改 yaml |

> 改 `.env` 后执行 `docker compose up -d`，compose 自动重建 env 变化的容器。

## 3. yaml 复核（`server/config.docker.yaml`）

密钥类和主要行为开关都已在 `.env`（见 §2），此处只核对 `.env` 未覆盖的残留项（下列项若已通过 `GVA_SYSTEM_ENV`/`GVA_OSS_TYPE`/`GVA_SSO_MAX_AGE_SEC`/`GVA_OPEN_CAPTCHA` 等变量设置，以 `.env` 为准）：

```yaml
system:
    env: public            # 可用 GVA_SYSTEM_ENV 覆盖
    db-type: pgsql         # 保持；可用 GVA_DB_TYPE 覆盖
    oss-type: local|aliyun-oss|...   # 可用 GVA_OSS_TYPE 覆盖，与 AK/SK 自动配套
    use-redis: true        # 客服链路强依赖，不能关（无 env 覆盖，保持 yaml）
    use-mongo: true        # 同上
    router-prefix: ""      # 可用 GVA_ROUTER_PREFIX 覆盖；改后需同步前端与脚本
sso:
    max-age-sec: 300       # 建议开启 SSO 链接时效(0=不校验)；可用 GVA_SSO_MAX_AGE_SEC 覆盖
captcha:
    open-captcha: 0        # 保持一直开启；可用 GVA_OPEN_CAPTCHA 覆盖
```

## 4. 构建与启动

```bash
docker compose build server
docker compose up -d
docker ps --format '{{.Names}} {{.Status}}' | grep gva    # 7 Up + kafka-init Exited
curl -s http://127.0.0.1:8888/health                       # "ok"
```

### 全新库初始化（标准 initdb 流程）

服务启动即连库，需临时制造 `GVA_DB=nil` 门控：
1. `config.docker.yaml` 临时置空 `pgsql.db-name` → `docker compose up -d server`
2. `POST /init/initdb`（dbType=pgsql、host=gva-pgsql、dbName=gva、adminPassword=强密码）
3. 恢复 `db-name` → `docker compose restart server` → `GET /init/checkdb` 应 `needInit:false`

> initdb 末尾报 `read-only file system` 属 `:ro` 挂载下 WriteConfig 失败，种子数据已全部落库，勿重跑。admin 首登立即改密。

### 商户/账号数据

平台后台（web 端 `/#/layout/csPlatform/merchants`）开通租户（code 自动追加 8 位 md5 后缀，以后端返回的完整 code 为准）；商户后台创建客服账号。演示种子账号密码 `123456` **仅限开发**，生产环境请删除或改密。

## 5. 前端构建与 nginx

```bash
cd chat-web     && npm ci && npm run build     # dist/: h5.html + workbench.html 双入口
cd merchant-web && npm ci && npm run build     # dist/
cd web          && npm ci && npm run build     # dist/: 平台后台
```

- **直连模式（不经 `/api` 反代）**：三端按各自 `.env.production` 构建，API base 填后端**绝对域名** `https://kfapi.laigoushangcheng.cn`（**不带 `/api`**——业务路由在根 `/base /user /merchant /member /agent`，framework autocode 路由已在 url 自带 `/api`，故 base 不能再含）：
  - chat-web：`VITE_API_BASE` + `VITE_WS_BASE`（见 `chat-web/src/common/api.js`）
  - merchant-web：`VITE_API_BASE`（见 `src/api/index.js` baseURL）
  - web：`VITE_BASE_API` / `VITE_FILE_API`（见 `src/utils/request.js` L129）
  - 未配置时均回退 `/api` 兼容 dev vite 代理，本地开发不受影响
- **nginx**：同域同时托管静态产物与后端——`location /`→前端 `dist`，业务路径（`/base /user /merchant /member /agent /api /cs`）`proxy_pass http://127.0.0.1:8888`（**不再 rewrite 去前缀**）
- **跨域必读**：三端实际按租户部署在动态子域（如 `https://kfadmin<hash>.laigoushangcheng.cn`），与 API 域 `kfapi.*` **不同源，会触发 CORS**（且浏览器对同源 POST 也带 `Origin`）。后端已启用 `CorsByRules` 并在 `cors.whitelist` 登记 `http(s)://*.laigoushangcheng.cn`（支持通配符，回显真实源）；新增前端子域只需保证命中该通配项
- **WS**：`location /ws { proxy_pass http://127.0.0.1:8889; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"; proxy_read_timeout 300s; }`，生产经 `VITE_WS_BASE=wss://kfapi.laigoushangcheng.cn/ws` 指定（不再按 `location.hostname:8889` 拼端口）
- 平台后台免二次登录依赖框架登录态，与客服 cstoken 同域部署无冲突（令牌域隔离是特性）

## 6. 上线安全核对清单（全部必查）

- [ ] `GVA_JWT_SIGNING_KEY` 已设强随机（默认 `qmPlus` 等于裸奔）
- [ ] `GVA_SMS_DEV_RETURN_CODE=false` 已显式设置（登录接口不回显 `devCode`）
- [ ] `GVA_CS_SSO_KEY` 为生产值且已同步商户对接方；`sso.max-age-sec` 已开启
- [ ] pgsql/redis 强密码、mongo 已启 auth（`GVA_MONGO_USERNAME/PASSWORD` 非空即开，**仅全新卷生效**；仅首次建卷时设值最省事，清空前注意消息数据）；中间件端口不对公网暴露
- [ ] admin/商户账号无 `123456` 弱口令
- [ ] HTTPS 全站；`system.env: public`
- [ ] 限流配额按业务 SLA 复核（默认：member 发消息 60/min、agent 120/min、登录 10~20/min、sso-login 20/min/IP）
- [ ] 已知缺口评估接受度：访客端登录无账号锁定（仅 IP 限流兜底）、渠道白名单未做服务端 Origin 校验——对外公网开放建议先上 WAF/IP 频控补偿（详见 security-ratelimit.md §8）

## 7. 部署后验证

```bash
bash scripts/e2e-cs.sh                    # 67 项全链路(BASE_URL 指向生产实例)
node scripts/e2e-notify.mjs               # 10 项事件推送
node scripts/e2e-targeted-notify.mjs      # 6 项定向提醒
```

手工冒烟：h5 账密登录建会话 → workbench 认领 → 互发消息 → WS 实时到达；SSO 链接 `h5.html?t=<code>&user=<密文>` 免密进入；文件上传/短链下载（5 分钟时效）。
脚本仅适合预发/灰度环境跑（会造数据），生产正式流量下改用只读冒烟。

## 8. 运维

- **备份**：`pg_dump`（每日）+ mongo `mongodump`（消息库）+ 对象存储桶（`oss-type=aliyun-oss` 后附件主体在此）；具名卷 `gva-server-uploads` 仅承载切换前的历史 local 文件，仍需保留备份；Redis 为可重建缓存（限流/验证码态），不作备份要求
- **日志**：`docker compose logs -f server`；级别可用 `.env` 的 `GVA_ZAP_LEVEL` 调（生产建议 warn）
- **升级**：`git pull && docker compose build server && docker compose up -d`（三应用进程共享镜像，一并重建）；DB 迁移由 GORM AutoMigrate 自动执行，升级前备份
- **令牌吊销**：当前无主动吊销通道（7d 过期），泄露处置=轮换 `GVA_JWT_SIGNING_KEY`（全员重登）
- **容量**：单实例消息链路 ~1000-1800 msg/s，瓶颈依次为业务限流 → server CPU → Kafka 单 broker → pgsql；扩容优先 server 水平复制 + Kafka 分区数（见 test-report.md）

### 按变更类型选最小部署动作（无需“覆盖全部 + down + up”）

`docker compose up -d` 本身只重建“定义有变化”的服务，未变更的容器不会动。**切勿在已有生产数据上敲 `down -v`**（会删掉 `gva-pgsql-data`/`gva-mongo-data` 等数据卷）。

| 改了什么 | 生效方式 | 要 down 吗 |
|---|---|---|
| **Go 代码**（如 CORS 中间件） | 二进制打进镜像 → `docker compose build server && docker compose up -d server storage-worker ws-gateway`（三者共用 `gva-server:pgsql`，必须一起重建；**仅 `restart` 不行**，会重跑旧二进制） | 否 |
| **config.docker.yaml** | `:ro` bind-mount，改文件不触发重建 → `docker compose restart server storage-worker ws-gateway` | 否 |
| **.env 变量** | 直接 `docker compose up -d`，compose 自动检测 env 变化重建对应容器 | 否 |
| **前端 dist**（chat-web/merchant-web/web） | 不在 compose 里，nginx 托管 → 覆盖各 `dist/` 站点目录 + `nginx -s reload` | 与 compose 无关 |

基础设施容器（pgsql/redis/mongo/kafka）本次未改就别 down，避免无谓连接中断与 mongo 副本集重选；`pgsql`/`mongo` 的重鉴权类变更只在**首次建卷**生效，勿随意 `down -v` 重建。

## 9. 回滚

镜像按 tag 保留上一版本（当前 `gva-server:pgsql` 为滚动覆盖式，建议发布时 `docker tag` 加版本号为习惯）：

```bash
docker tag gva-server:pgsql gva-server:rel-YYYYMMDD   # 发布前留档
docker compose down server storage-worker ws-gateway
docker tag gva-server:<旧版本> gva-server:pgsql
docker compose up -d server storage-worker ws-gateway
```

AutoMigrate 只加表加列不回滚，字段级不兼容变更需按备份恢复 pgsql/mongo。
