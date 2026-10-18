# StarNexus 生产架构与发布手册

> 2026-10-18 用户明确确认：**StarNexus 仅部署到 S2**。此约定替代此前多应用节点发布流程。

## 当前职责

| 节点 | 用途 | StarNexus 发布目标 |
| --- | --- | --- |
| S2：`sn-s2` / `192.243.114.184` | StarNexus 入口，`xingyuapi-new-api`，节点名 `xingyuapi-prod-2`，Origin `origin-s2.dkby.com` | **唯一目标** |
| DB7：`sn-db7` / `192.243.115.154` | PostgreSQL `db7-postgres`、Redis `db7-redis` | 数据依赖，不随应用发布重建 |
| S4：`sn-s4` / `85.155.178.254` | 本次渠道 86 使用的上游 Sub2API 和内网 Wrapper | 不属于 StarNexus 发布范围 |
| S1 / S3 | 历史节点标识 | 不部署、不重启 |

其他服务器残留同名容器、数据库 `routing_nodes` 记录、旧 Compose 注释或历史多节点发布记录，不构成当前发布授权。排查上游服务也不等于发布 StarNexus 到上游节点。

当前链路：用户 → S2 StarNexus → S4 Sub2API → 内网 Wrapper；S2 的数据库和 Redis 使用 DB7。

## 生产配置来源

- S2 工作目录：`/www/wwwroot/starnexus`；基础配置：`docker-compose.prod2.yml`。
- 读取当前容器 `com.docker.compose.project.config_files` 标签，保留所有生效的覆盖文件；不能只用基础 Compose 重建。
- 以服务器实际 `.env` 和容器配置为准；不得用仓库历史 `.env.prod*` 覆盖。
- 数据库名 `starnex`；PostgreSQL 地址 DB7:5432；Redis 地址 DB7:6379/1。凭据不写入文档或日志。
- S2 当前 `NODE_TYPE=slave` 是已有运行配置。仅修正文档或部署镜像时不要改角色，也不要因此自动启动 S1 或安排迁移。

## 发布流程

1. 在独立检出目录整理本次范围内的改动，完成测试、检查差异；保留其他工作区未提交内容。
2. 提交并推送，等待 `.github/workflows/docker-image-main.yml` 构建成功。生产节点不执行完整源码 Docker 构建。
3. 记录提交 SHA、Actions URL、镜像 digest；生产使用 `sha-<完整提交>` 固定标签，不追随浮动 `main`。
4. **仅在 S2** 保存旧镜像 ID、Compose 文件列表、健康状态及回滚镜像标签。
5. 拉取目标镜像，核对 OCI revision 与 Actions digest；通过额外覆盖文件只替换镜像，保留现有环境变量、挂载、网络和停止等待设置。
6. 用原 Compose 文件列表加本次覆盖文件，执行 `up -d --no-deps --force-recreate --pull never new-api`。尊重现有优雅退出等待时间；重建会短暂影响入口可用性。
7. 验证 S2 容器 healthy、版本和二进制哈希正确、Origin `/api/status` 成功；观察至少 30 秒，再检查至少 3 分钟业务日志及本次功能验收。
8. 记录生产验证结果。**流程到 S2 结束，不继续 S1/S3/S4。**

## 回滚

健康或功能验收失败时，用保存的旧镜像和原 Compose 配置在 S2 重建，再验证健康与业务。此流程不修改数据库或渠道配置。涉及 schema 的独立变更应另行审查，不套用本次无迁移发布流程。

## 本次 Messages 修复

应用提交：`174cba24580b32783af1616f536384867bf9dcdc`。

修复正常终止后的断连误报，并排除心跳对首内容计时的影响。没有新增渠道开关，不修改请求、历史、缓存控制或重试策略。验收需包含：读取到 EOF、收到 message_stop 后立即关闭、终止前取消，以及缓存 usage 保持不变。

## 只读排查

```sh
ssh sn-s2 'docker ps --filter name=xingyuapi-new-api'
ssh sn-s2 'docker logs --since 10m xingyuapi-new-api'
ssh sn-s2 'docker exec xingyuapi-new-api sha256sum /new-api'
```

数据查询使用 DB7 的 `db7-postgres`，按实际 POSTGRES_USER 连接 `starnex`，优先 `BEGIN READ ONLY` 并设置 statement_timeout。渠道 86 的上游日志在 S4 的 Sub2API 和内网 Wrapper 查询；这是上游诊断，不改变 StarNexus 部署范围。

凭据、DSN 密码和隧道 token 不得进入文档、提交或聊天。历史多节点拓扑保留于 Git 历史，不作为现行操作手册。
