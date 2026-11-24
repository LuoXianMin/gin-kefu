aaS 在线客服系统开发执行方案 版本：V1.2 用途：直接交给 AI Agent 执行项目开发 部署方式：开发、测试和生产均使用 Docker，不使用 Kubernetes 或 Helm。
项目目标 建设生产级、多租户 SaaS 在线客服系统，包含：
访客 H5 用户端,用vue3。
客服工作台 vue3 样式现代化。
平台管理后台 用AntDesign vue3。
Go 后端服务及异步 Worker。
Docker 开发、测试和生产部署配置。 支持企业独立管理客服、技能组、访客、会话、消息、工单和运营数据。
5 万～20 万在线 WebSocket 连接是架构设计和压力测试目标，不是未经测试的性能承诺。 2. 执行边界 Agent 负责软件开发、Docker 镜像构建、自动化验证和部署材料准备。
未经人工明确批准，Agent 不得：
购买云资源，或创建、修改、删除阿里云生产资源。
修改生产 VPC、安全组、WAF、CDN、SLB 或生产数据库。
连接生产服务器、执行生产发布或生产数据库迁移。
将真实密钥写入代码、镜像、日志或提交记录。 阿里云 OSS 使用私有 Bucket。Agent 可实现接入代码和配置模板，不得擅自创建或修改生产 OSS 资源。
固定技术选型 不得擅自替换以下核心技术： 模块 技术 编程语言 Go API 网关 Gin 内部通信 gRPC + Protobuf WebSocket 
独立 Go WebSocket Gateway ORM GORM 高性能 SQL pgx 主数据库 PostgreSQL 消息队列 Kafka、franz-go 消息查询存储
MongoDB 缓存 Redis Cluster 文件存储 阿里云 OSS Go SDK V2 监控 Prometheus + Grafana 链路追踪
OpenTelemetry 容器与部署 Docker、Docker Compose
采用模块化业务架构，避免初期过度拆分微服务。部署单元至少包括 API Gateway、WebSocket Gateway、Core Business Service、Message Worker、Outbox Worker、Notification Worker。Analytics Worker 可按需拆分。 4. 系统功能 4.1 访客 H5 实现匿名身份生成、会话恢复、文字聊天、图片和文件发送、消息状态、已读与撤回、历史记录、客服输入及在线状态、自动重连、按消息序号补拉、会话结束和满意度评价。
访客身份使用服务端签发的凭证，不信任客户端自行提交的普通 ID。 4.2 客服工作台 实现客服登录与 MFA、在线状态、会话列表和新会话提醒、自动及手动接待、会话转接、技能组分配、消息收发、图片和文件预览、已读与撤回、快捷回复、访客信息、历史会话、标签、备注、会话关闭、工单创建与转派、客服工作量统计。 4.3 管理后台 支持以下角色及权限范围：
平台超级管理员：管理全部租户、平台配置、套餐、全局审计和平台运营。
企业管理员：管理本企业客服、部门、角色、渠道、业务配置和报表。
客服主管：管理本团队客服、技能组、接待分配、会话质检和工单。 采用 RBAC 和数据范围控制，防止企业管理员访问其他租户数据。
多租户和数据设计 所有企业业务数据必须带有 tenant_id。不得只依赖业务代码中的查询条件实现隔离。
必须实现：
认证后建立可信租户上下文，服务端验证用户所属租户。
Repository 层统一施加租户约束。
PostgreSQL RLS 作为纵深防御。
异步任务携带租户上下文。
文件、消息、报表和导出操作验证租户及数据范围。
不信任客户端直接传递的 tenant_id。 至少建立 tenants、tenant_settings、users、user_roles、roles、permissions、role_permissions、api_keys、visitors、visitor_identities、agents、agent_skills、skill_groups、agent_skill_groups、agent_presence、conversations、conversation_members、conversation_assignments、conversation_tags、messages、message_receipts、read_cursors、outbox、idempotency_records、tickets、ticket_comments、ticket_assignments、audit_logs 等表。
数据库要求：
主键和时间字段采用统一规范，时间统一按 UTC 存储。
重要业务表包含 tenant_id，关键查询建立组合索引。
消息表建立 conversation_id + seq 唯一索引。
幂等表建立租户、操作者、幂等键唯一约束。
生产迁移采用向前兼容方式；大表迁移评估锁和索引构建影响。
GORM 用于普通 CRUD；pgx 用于消息写入、会话序号分配、Outbox、批量写入、高并发事务和复杂 SQL。
消息可靠性 消息链路采用 PostgreSQL、Outbox、Kafka 和 MongoDB：
PostgreSQL 是业务事实源。
消息和 Outbox 在同一 PostgreSQL 事务中提交。
Kafka 使用 conversation_id 作为消息 Key。
Kafka 消费采用至少一次投递。
MongoDB 根据 message_id 幂等 upsert。
Consumer 处理成功后再提交 offset；失败进入重试及 DLQ。
客户端按消息 ID 和 seq 去重。
MongoDB 投影延迟或不可用时，按策略从 PostgreSQL 回源。
不得宣称 PostgreSQL、Kafka 和 MongoDB 之间实现 Exactly-once。
WebSocket 高并发 独立实现 WebSocket Gateway，支持：
WSS、连接鉴权、Origin 白名单、短期连接凭证。
心跳、Ping/Pong、断线重连和按 seq 补拉。
每个连接一个读协程、一个写协程和有界发送队列。
WriteDeadline、ReadLimit、慢消费者断开。
使用 Redis 维护连接路由，禁止每条消息全量扫描连接。
服务扩容及下线时支持连接排空和优雅退出。
客服自动分配 分配时必须检查客服在线、所属租户、技能匹配、接待上限及启用状态。按负载率排序，同等条件按最近分配时间公平分配。
使用 Redis Lua 实现原子预占，由 PostgreSQL 记录最终分配结果。预占失败、数据库事务失败或客服离线时，必须有补偿机制。 9. OSS 文件管理 禁止使用 MinIO。文件存储统一使用阿里云 OSS 私有 Bucket。
上传流程：
客户端向 API 申请上传。
服务端验证租户配额、文件类型和大小。
服务端生成 Object Key 并签发短时效 PUT URL。
客户端直接上传 OSS。
客户端通知上传完成，服务端通过 HeadObject 校验。
文件进入安全扫描状态，扫描通过后才能变为 available。
消息只能引用 available 状态文件。 下载时，服务端必须验证租户归属、会话成员权限及文件状态，再签发短时效 GET URL。建议上传 URL 有效期 5～10 分钟、下载 URL 有效期 5 分钟。图片、普通文件、视频大小限制分别建议为 10 MiB、50 MiB、200 MiB，并支持租户级配置。
安全要求 必须实现：
管理员 MFA、Refresh Token 轮换、重用检测、Token 撤销和设备会话管理。
登录失败、API 请求和 WebSocket 消息频率限制。
文件权限校验、RBAC、跨租户隔离和敏感操作审计。
防 SQL 注入、XSS、SSRF、IDOR/BOLA。
导出操作限额和敏感信息日志脱敏。 生产容器必须：
使用非 Root 用户运行。
应用支持时启用只读根文件系统。
禁止权限提升，删除不必要的 Linux capabilities。
使用 seccomp，限制容器间网络访问。
通过环境变量或外部 Secret 管理注入密钥。
扫描镜像漏洞，避免镜像包含真实密钥和不必要的构建工具。
监控与降级 监控在线连接数、WebSocket 握手成功率、消息发送速率与 ACK 延迟、Outbox 积压、Kafka Consumer Lag、Mongo 投影延迟、Redis 错误率、PostgreSQL 连接池等待、API p95/p99、登录失败、跨租户访问拒绝和文件扫描积压。
故障降级原则： 故障 降级策略 Redis 故障 已提交消息不能丢失；自动分配降级 Kafka 故障 Outbox 暂存并告警 MongoDB 故障 按策略从 PostgreSQL 回源 WebSocket 故障 客户端重连并补拉 PostgreSQL 故障 不得假报消息发送成功 OSS 故障 文件状态保持失败或待处理
Docker 与生产部署 开发、测试和生产均使用 Docker。生产使用 Docker Compose 编排，不使用 Kubernetes 或 Helm。
仓库必须提供：
Go 服务多阶段 Dockerfile。
三端前端的构建及容器运行配置。
开发用 Docker Compose 配置。
独立的生产 Compose 配置及变量模板。
.env.example，只包含变量名和非敏感示例值。
健康检查、服务网络、持久化卷、日志配置和重启策略。
数据库迁移、MongoDB 索引初始化的可重复执行入口。
Docker 镜像构建、标签和本地运行说明。
生产部署、升级、回滚、备份及恢复说明。 Docker 要求：
Go 使用多阶段构建，运行镜像尽量精简。
前端构建阶段与静态运行阶段分离。
服务通过环境变量配置，不在镜像中写入环境专属配置。
容器使用非 Root 用户，镜像不得包含仓库凭证或真实密钥。
配置健康检查和依赖重试，不依赖固定时长 sleep。
数据目录使用命名卷持久化；默认停止流程不得删除业务数据。
docker compose down -v 只能作为明确的数据清理操作说明。
Compose 文件不得内置生产密钥、生产域名、云资源 ID 或默认公网入口。 本地基础设施容器包括 PostgreSQL、Kafka、MongoDB、Redis，以及开发所需的 Prometheus 和 Grafana。禁止添加 MinIO。
Agent 只负责生成和验证镜像及 Compose 配置，不得连接生产服务器或执行生产发布；生产部署由人工审核配置后执行。 13. 交付物 Agent 必须交付：
完整 Go 源代码和三端接口定义。
PostgreSQL 建表及迁移脚本。
MongoDB 索引初始化脚本。
Kafka Topic 配置说明。
Redis Key 设计文档。
WebSocket 协议文档。
OpenAPI 文档和 Protobuf 文件。
OSS 接入代码。
Dockerfile、开发 Compose 和生产 Compose 配置。
.env.example 和完整 README。
自动化测试、安全测试和压力测试脚本。
备份恢复脚本和故障演练手册。
Grafana 仪表盘及告警规则示例。
功能、安全、故障恢复和性能验证报告。
执行阶段 阶段 工作内容 完成标准 A 架构设计 架构图、模块设计、数据模型、容器拓扑 B 工程初始化 服务可构建，Docker Compose 可启动 C 租户和权限 RBAC、MFA、RLS 和隔离验证 D 会话和消息 消息、Outbox、Kafka、Mongo 投影和补拉 E WebSocket 长连接、鉴权、重连、补拉和优雅退出 F 客服业务 分配、转接、工单和工作台功能 G OSS 上传、扫描、权限校验和下载 H 安全和监控 安全验证、指标、追踪、仪表盘和告警 I Docker 部署 生产 Compose、升级、回滚、备份恢复文档 J 综合验收 功能、隔离、安全、故障及性能报告
每阶段完成后，Agent 应提供交付物、实际验证结果和未解决问题，再进入下一阶段。未通过的项目不得标记为已完成。 15. Agent 工作规则
开始前检查仓库结构、已有代码和项目约定，保留用户已有改动。
先提交架构和执行清单，再逐阶段实现，不得只搭空架构或占位业务。
完成功能后运行相关构建和自动化验证，并报告实际命令及结果。
不得静默跳过需求；无法完成时说明阻塞、影响和建议。
不得输出或提交真实密钥、个人数据和生产日志。
最终 README 必须说明 Docker 本地启动、生产配置、部署、升级、回滚、备份恢复、测试结果、已知限制及人工生产发布步骤。 优先做服务端