# StarNexus 生产集群架构与新增服务器流程

> 最后核验：2026-08-02。
>
> 本文以生产节点的容器运行参数和正式数据库 `routing_nodes` 记录为准。此次核验没有登录 S2；S2 的地址来自本机 SSH 配置，路由状态来自正式数据库。

## 当前架构

| 节点 | SSH 别名 / 公网 IP | 运行角色 | 对外入口与状态 | 部署位置 |
| --- | --- | --- | --- | --- |
| S1 | `starnexus-s1` / `104.194.82.238` | `xingyuapi-prod-1`，`master` | `origin-s1.dkby.com`，启用、可见、监控启用 | `/www/wwwroot/starnexus/docker-compose.prod.yml` |
| S2 | `starnexus-s2` / `149.104.12.60` | 本次未登录核验 | `origin-s2.dkby.com`，启用、隐藏、监控关闭 | 本次未核验，不应据此文档推断其运行配置 |
| S3 | `starnexus-s3` / `95.169.7.175` | `xingyuapi-prod-3`，`slave` | `origin-s3.dkby.com`，启用、可见、监控启用 | `/www/wwwroot/starnexus/docker-compose.prod2.yml` |
| S4 | `starnexus-s4` / `149.104.11.117` | `xingyuapi-prod-4`，`slave` | `origin-s4.dkby.com`，启用、可见、监控启用 | `/www/wwwroot/starnexus/docker-compose.prod2.yml` |
| DB6 / SPG | `starnexus-db6` / `67.230.183.84` | 正式 PostgreSQL、PgBouncer、Redis | 正式库路由键 `spg`，启用、可见、监控启用 | `/srv/db-stack/docker-compose.yml` |

当前访问链路：

```text
用户
  |
Cloudflare / 节点路由
  |
S1 master        S2（本次未核验）       S3 slave        S4 slave
  |                     |                   |               |
  +---------------------+-------------------+---------------+
                                |
                         DB6 / SPG 正式数据节点
                         PgBouncer :6432
                         PostgreSQL :5432
                         Redis      :6379/1
```

## 正式数据库与 Redis

所有已核验的生产业务容器（S1、S3、S4）实际连接 DB6，不连接 S2，也不连接 S3 上的本地 PostgreSQL/Redis 容器。

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

## 当前路由节点状态

正式数据库中的 `routing_nodes` 当前记录为：

| 路由键 | 类型 | Origin | 启用 | 可见 | 监控 |
| --- | --- | --- | --- | --- | --- |
| `s1` | application | `origin-s1.dkby.com` | 是 | 是 | 是 |
| `s2` | application | `origin-s2.dkby.com` | 是 | 否 | 否 |
| `s3` | application | `origin-s3.dkby.com` | 是 | 是 | 是 |
| `s4` | application | `origin-s4.dkby.com` | 是 | 是 | 是 |
| `spg` | database | 无业务 Origin | 是 | 是 | 是 |

## 部署约定

S1 使用主节点 Compose：

```bash
cd /www/wwwroot/starnexus
docker compose -f docker-compose.prod.yml pull new-api
docker compose -f docker-compose.prod.yml up -d --no-deps --force-recreate new-api
```

S3、S4 使用从节点 Compose：

```bash
cd /www/wwwroot/starnexus
docker compose -f docker-compose.prod2.yml pull new-api
docker compose -f docker-compose.prod2.yml up -d --no-deps --force-recreate new-api
```

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
