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

## 6. 生产部署要点

- 仓库自带编排样例：`deploy/docker/`、`deploy/docker-compose/`、`deploy/kubernetes/`（server/web 两套 k8s yaml）；本项目的根 `docker-compose.yml` 为当前实测版本，优先参考。
- 必须调整：
  1. `system.jwt-key` 与 cstoken 密钥更换为生产随机值；`config.docker.yaml` 中数据库/Redis/Mongo 口令改强密码并去掉 :ro 挂载依赖（或改用 ConfigMap/Secret）。
  2. 前端 `csPlatform` 接入链接的 `:8090` dev 端口写死（`web/src/view/csPlatform/index.vue` 的 `chatBase`）需替换为正式域名。
  3. 生产关闭 dev 回显（`devCaptcha/devCode` 由 sms/captcha 配置开关控制），接入真实短信通道。
  4. 限流值按业务 SLA 复核（访客发消息 60/min/账号、客服 120/min、登录 10/min/IP、渠道查询 30/min/IP）。
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
