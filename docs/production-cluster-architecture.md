# StarNexus 生产集群架构与部署手册

> 架构最后核验：2026-08-09；发布流程最后实测：2026-08-09。
>
> 本文以生产节点实际容器、服务器本地 Compose 与正式数据库 `routing_nodes` 记录为准。仓库内历史 `.env.prod*` 不能作为生产配置来源。

## 当前架构

| 节点 | SSH 别名 / 公网 IP | 运行角色 | 对外入口与状态 | 部署位置 |
| --- | --- | --- | --- | --- |
| S1 | `starnexus-s1` / `104.194.82.238` | `xingyuapi-prod-1`，`master` | `origin-s1.dkby.com`；2026-08-09 发布后健康、Origin 200 | `/www/wwwroot/starnexus/docker-compose.prod.yml` |
| S2 | `starnexus-s2` / `45.132.74.201` | `xingyuapi-prod-2`，`slave` | `origin-s2.dkby.com`；2026-08-22 更换主机后健康、Origin 200 | `/www/wwwroot/starnexus/docker-compose.prod2.yml` |
| S3 | `starnexus-s3` / `95.169.7.175` | `xingyuapi-prod-3`，`slave` | `origin-s3.dkby.com`；2026-08-09 发布后健康、Origin 200 | `/www/wwwroot/starnexus/docker-compose.prod2.yml` |
| S4 | `starnexus-s4` / `85.155.178.254`（VPS004） | `xingyuapi-prod-4`，`slave` | `origin-s4.dkby.com`；生产节点 | `/www/wwwroot/starnexus/docker-compose.prod2.yml` |
| DB6 / SPG | `starnexus-db6` / `67.230.183.84` | 正式 PostgreSQL、PgBouncer、Redis | 正式库路由键 `spg`，启用、可见、监控启用 | `/srv/db-stack/docker-compose.yml` |

当前访问链路：

```text
用户
  |
Cloudflare / 节点路由
  |
S1 master             S2 slave             S3 slave        S4 slave
  |                     |                   |               |
  +---------------------+-------------------+---------------+
                                |
                         DB6 / SPG 正式数据节点
                         PgBouncer :6432
                         PostgreSQL :5432
                         Redis      :6379/1
```

## 正式数据库与 Redis

所有已核验的生产业务容器（S1、S2、S3、S4）实际连接 DB6，不连接业务节点上的本地 PostgreSQL/Redis 容器。

```env
SQL_DSN=postgresql://starNex:<POSTGRES_PASSWORD>@67.230.183.84:6432/starnex
REDIS_CONN_STRING=redis://:<REDIS_PASSWORD>@67.230.183.84:6379/1
```

关键约定：

- 正式数据库名是小写 `starnex`，数据库用户是 `starNex`。
- 业务应用统一通过 DB6 的 PgBouncer `6432` 端口连接 PostgreSQL。
- DB6 的 PostgreSQL `5432` 是数据库服务端口，不应替代业务应用使用的 `6432`。
- DB6 运行 `db-postgres`、`db-pgbouncer`、`db-redis`，均使用 host network。
- S3 当前还能看到 `s3-postgres`、`s3-redis` 容器，但它们不是当前 StarNexus 正式业务容器的数据源。
- 仓库中的 `.env.prod`、`.env.prod2`、`.env.prod3` 含历史拓扑，不能作为当前生产数据库地址或密码来源；部署前必须核对服务器上的实际 `.env`。

## 核心规则

- 生产环境只能有一个 `NODE_TYPE=master`，当前是 S1。
- 其它生产业务节点使用 `NODE_TYPE=slave`。
- 每个业务节点的 `NODE_NAME` 必须唯一。
- 所有生产业务节点的 `SESSION_SECRET` 和 `CRYPTO_SECRET` 必须一致。
- 新业务节点必须继续连接 DB6 的 PgBouncer 和 Redis，不能在业务节点上另建正式数据源。
- 数据库和 Redis 的防火墙只允许已登记业务节点访问，禁止向全网开放。

## 路由节点配置基线

以下为 2026-08-02 正式数据库中的 `routing_nodes` 核验记录。发布前后应在管理后台重新确认，不应把此表当作永久状态：

| 路由键 | 类型 | Origin | 启用 | 可见 | 监控 |
| --- | --- | --- | --- | --- | --- |
| `s1` | application | `origin-s1.dkby.com` | 是 | 是 | 是 |
| `s2` | application | `origin-s2.dkby.com` | 是 | 否 | 否 |
| `s3` | application | `origin-s3.dkby.com` | 是 | 是 | 是 |
| `s4` | application | `origin-s4.dkby.com` | 是 | 是 | 是 |
| `spg` | database | 无业务 Origin | 是 | 是 | 是 |

## 唯一标准生产发布流程

标准流程必须是：

```text
提交全部计划发布的工作区改动
  -> 推送 GitHub main
  -> GitHub Actions 构建 linux/amd64 镜像并推送 GHCR
  -> S2 灰度部署并观察
  -> S1 主节点部署并观察迁移
  -> S3 部署
  -> 三节点联合验收
```

不允许在生产服务器上执行完整源码 `docker build`。2026-08-09 曾在 S2 触发高负载，系统 load average 升至约 33，并出现 SSH banner 超时和 Origin 短暂 502。生产节点只能拉取已经由 GitHub Actions 构建完成的镜像。

### 1. 提交前检查

确认工作区中所有计划发布的改动均已纳入提交，临时部署产物（例如 `.codex-deploy/`）不得提交：

```bash
git status --short
git diff --check
```

按改动范围完成 Go 测试和前端检查。前端使用 Bun：

```bash
go test ./...
cd web/default
bun run build
```

回到仓库根目录提交并推送 `main`：

```bash
git add <本次计划发布的文件>
git commit -m "<提交说明>"
git push origin main
```

### 2. 等待 GitHub Actions 完成

工作流 `.github/workflows/docker-image-main.yml` 在 `main` 推送后自动执行：

- 构建平台：`linux/amd64`；
- 镜像：`ghcr.io/z55183980-gif/starnexus`；
- 标签：`main` 和 `sha-<完整提交 SHA>`；
- 应用版本：`main-<7 位提交 SHA>`；
- GHCR 登录：GitHub Actions 内置 `GITHUB_TOKEN`，权限为 `packages: write`。

用 GitHub CLI 等待并确认构建成功：

```bash
gh run list --workflow docker-image-main.yml --branch main --limit 5
gh run watch <run-id> --exit-status
```

Actions 未成功前禁止部署。记录本次提交 SHA、Actions Run URL 和 GHCR 镜像 digest。日常发布不需要从开发机或生产机手工推 GHCR；个人令牌只用于应急，且必须包含 `write:packages`。

### 3. 部署前基线和回滚点

按节点执行以下只读检查，记录旧镜像 ID、容器健康状态、资源占用和 Origin 状态：

```bash
uptime
docker inspect xingyuapi-new-api \
  --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} {{.Image}} {{.Config.Image}}'
docker stats --no-stream xingyuapi-new-api
curl -fsS https://origin-sN.dkby.com/api/status
```

部署前为正在运行的镜像建立本地回滚标签。先读取并人工核对镜像 ID，再使用明确值打标签，避免跨 PowerShell/SSH 的命令替换和引号问题：

```bash
docker inspect xingyuapi-new-api --format '{{.Image}}'
docker tag <旧镜像 ID> starnexus:rollback-sN-before-<7位提交SHA>
```

### 4. 串行部署 S2 -> S1 -> S3

每个节点必须完成部署和观察后才能进入下一个节点。当前 Compose 对应关系：

| 顺序 | 节点 | 角色 | Compose 文件 | 说明 |
| --- | --- | --- | --- | --- |
| 1 | S2 | slave | `docker-compose.prod2.yml` | 灰度节点，先验证容器、Origin、日志和路由监控 |
| 2 | S1 | master | `docker-compose.prod.yml` | 唯一执行数据库迁移的业务节点 |
| 3 | S3 | slave | `docker-compose.prod2.yml` | 前两节点通过后部署 |

S4 是生产 slave。发布范围包含 S4 时，在 S3 验收后重新核验 S4 基线，再使用 `docker-compose.prod2.yml` 部署。

S2 和 S3：

```bash
cd /www/wwwroot/starnexus
docker pull ghcr.io/z55183980-gif/starnexus:main
docker image inspect ghcr.io/z55183980-gif/starnexus:main \
  --format '{{.Id}} {{json .RepoDigests}}'
docker compose -f docker-compose.prod2.yml up -d \
  --no-deps --force-recreate --pull never new-api
```

S1：

```bash
cd /www/wwwroot/starnexus
docker pull ghcr.io/z55183980-gif/starnexus:main
docker image inspect ghcr.io/z55183980-gif/starnexus:main \
  --format '{{.Id}} {{json .RepoDigests}}'
docker compose -f docker-compose.prod.yml up -d \
  --no-deps --force-recreate --pull never new-api
```

必须核对 `docker image inspect` 返回的 RepoDigest 与 Actions 产物一致。显式 `docker pull` 后使用 `--pull never`，可确保重建过程使用刚刚核验过的本地镜像，避免二次拉取期间 `main` 标签变化。

### 5. 每个节点的观察门槛

每个节点至少持续观察 30 秒；三个节点全部完成后，再持续检查生产端用户日志 3 分钟。检查项：

```bash
docker inspect xingyuapi-new-api \
  --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} {{.Image}}'
docker exec xingyuapi-new-api sha256sum /new-api
docker logs --since 3m xingyuapi-new-api
docker stats --no-stream xingyuapi-new-api
curl -fsS https://origin-sN.dkby.com/api/status
```

验收必须同时满足：

- 容器状态为 `running healthy`；
- S1/S2/S3 本地镜像 ID 或 RepoDigest 完全一致，并等于本次 Actions 产物；
- `/new-api` 文件哈希三节点一致；
- `/api/status` 返回 HTTP 200 且业务状态成功；
- 管理后台路由监控持续产生节点报告，没有新增不可达告警；
- 3 分钟用户日志中没有新增 API 5xx、panic、数据库连接失败或持续重启；
- CPU、内存和 load average 没有持续异常上升。

单条 `stream ended: reason=client_gone` 或 `context canceled` 通常表示客户端主动断开，不应单独判定为服务故障；仍需结合 5xx、重启和请求成功率判断。

### 6. 回滚

任一节点未通过验收时停止后续节点部署。将该节点之前保存的回滚镜像重新标记为 Compose 使用的镜像名，然后禁止拉取地重建：

```bash
docker tag starnexus:rollback-sN-before-<7位提交SHA> \
  ghcr.io/z55183980-gif/starnexus:main
docker compose -f <该节点的Compose文件> up -d \
  --no-deps --force-recreate --pull never new-api
```

回滚后重新执行完整验收。S1 可能已经执行数据库迁移；回滚前必须确认旧版本与新 schema 兼容。不要自动回滚破坏性数据库变更，应先恢复服务或采用向前修复。

## 2026-08-09 已验证发布记录

| 项目 | 结果 |
| --- | --- |
| Git 提交 | `443969ca898eef99804cce166bc56cae331c467d` |
| Actions Run | `31315247366`，成功，用时 4 分 28 秒 |
| Actions 页面 | `https://github.com/z55183980-gif/starnexus/actions/runs/31315247366` |
| GHCR digest | `sha256:55e15edfa71ca872b3fda3fd786315501baa6c8e1beebcb7a90333186843fe22` |
| 应用版本 | S1/S2/S3/S4 均为 `main-af78935` |
| 镜像 RepoDigest | S1/S2/S3/S4 均为 `sha256:69881bace63d26494c5a6c02c29c1e0c915afac177a391649cd904c40e2fdb3c` |
| `/new-api` SHA-256 | S1/S2/S3 均为 `5a070e59b7e306dc38c6d1c189dae85cae6df36f97cb5f231dc0926b24c2b979`；S4 待本次部署验收后补录 |
| 容器与 Origin | S1/S2/S3 均为 healthy，三个 Origin 均返回 200；S4 待本次部署验收 |
| 生产日志 | S1/S2/S3 已完成发布观察；S4 待本次部署观察 |

本次观察到的非阻断事项：

- S1 启动阶段在 `model/user_affiliate.go` 出现两条既有 `idx_user_affiliates_aff_code` 重复键日志，但迁移完成、容器健康、请求正常。后续应单独清理重复数据/迁移幂等问题，不应在发布现场直接删数据。
- Actions 提示 Node.js 20 弃用并将固定版本 Actions 强制运行于 Node.js 24；本次构建不受影响，但后续应升级对应 Actions 固定版本。
- 如果应急复制 Windows 上生成的 Linux 二进制构建覆盖镜像，必须在 Dockerfile 中执行 `chmod 0755 /new-api`；否则容器可能因 `permission denied` 无法启动。此方案仅用于应急，不能替代标准 Actions 构建。

不要直接把仓库中的历史 `.env.prod*` 复制成生产 `.env`。部署前至少核对以下非敏感项，并从安全的密码来源填充秘密值：

```env
NODE_TYPE=master|slave
NODE_NAME=xingyuapi-prod-<唯一编号>
SQL_DSN=postgresql://starNex:<POSTGRES_PASSWORD>@67.230.183.84:6432/starnex
REDIS_CONN_STRING=redis://:<REDIS_PASSWORD>@67.230.183.84:6379/1
```

## 新增业务服务器流程

1. 将项目部署到新服务器的 `/www/wwwroot/starnexus`。

2. 创建服务器本地 `.env`，把正式数据目标设置为 DB6：

   ```env
   SQL_DSN=postgresql://starNex:<POSTGRES_PASSWORD>@67.230.183.84:6432/starnex
   REDIS_CONN_STRING=redis://:<REDIS_PASSWORD>@67.230.183.84:6379/1
   NODE_TYPE=slave
   NODE_NAME=xingyuapi-prod-<唯一编号>
   ```

3. 确认 `SESSION_SECRET` 和 `CRYPTO_SECRET` 与现有生产业务节点一致。

4. 在 DB6 的主机防火墙和云安全组中，仅为新业务节点放行：

   ```text
   6432/tcp  PgBouncer
   6379/tcp  Redis
   ```

   普通业务节点不需要直接访问 PostgreSQL `5432`；只有明确的数据库管理或复制用途才能单独放行。

5. 从新业务节点执行只读连通性检查：

   ```bash
   nc -vz 67.230.183.84 6432
   nc -vz 67.230.183.84 6379
   psql -h 67.230.183.84 -p 6432 -U starNex -d starnex
   redis-cli -h 67.230.183.84 -p 6379 -a '<REDIS_PASSWORD>' -n 1 ping
   ```

6. 使用从节点 Compose 启动业务容器：

   ```bash
   cd /www/wwwroot/starnexus
   docker compose -f docker-compose.prod2.yml pull new-api
   docker compose -f docker-compose.prod2.yml up -d --no-deps --force-recreate new-api
   docker ps --filter name=xingyuapi-new-api
   docker logs --tail 100 xingyuapi-new-api
   ```

7. 通过管理后台登记对应的路由节点和 Origin，并确认节点监控正常。不要直接在正式库中手工写 `routing_nodes`。

## 只读排查命令

查看业务节点容器与日志：

```bash
ssh starnexus-s1 'docker ps --filter name=xingyuapi-new-api'
ssh starnexus-s3 'docker logs --since 30m xingyuapi-new-api'
ssh starnexus-s4 'docker logs --since 30m xingyuapi-new-api'
```

确认业务容器实际数据目标时，只显示脱敏后的连接地址，不要把密码复制到终端记录或聊天中。

在 DB6 容器内执行正式库只读查询：

```bash
ssh starnexus-db6
docker exec -e PGOPTIONS='-c default_transaction_read_only=on' \
  -u postgres db-postgres \
  psql -X -v ON_ERROR_STOP=1 -d starnex
```

进入 `psql` 后仍建议显式使用只读事务：

```sql
BEGIN TRANSACTION READ ONLY;
SELECT current_database(), current_user;
-- 只执行 SELECT / EXPLAIN，不执行写入或 DDL。
COMMIT;
```

## DB6 检查清单

DB6 的正式数据栈位于 `/srv/db-stack/docker-compose.yml`。常用只读检查：

```bash
docker ps --filter name=db-postgres --filter name=db-pgbouncer --filter name=db-redis
ss -lntp | grep -E ':(5432|6432|6379)\b'
```

预期服务：

```text
db-postgres   PostgreSQL 18
db-pgbouncer  PgBouncer，业务入口 6432
db-redis      Redis 8，StarNexus 使用 DB 1
```

不要把以下端口开放给 `0.0.0.0/0`：

```text
5432/tcp
6432/tcp
6379/tcp
```

## 安全备注

- 数据库密码、Redis 密码、Cloudflare Tunnel token 和 API 密钥不得写入文档、提交记录、日志摘录或聊天。
- 如果密码或 token 曾进入聊天、日志或截图，应立即轮换。
- 架构排查优先使用节点别名、容器名和脱敏 DSN，避免复制秘密值。
