# StarNexus 生产集群架构与新增服务器流程

这份文档记录当前生产架构、服务器分工，以及以后新增业务服务器时需要执行的步骤，避免每次重新排查。

## 当前架构

| 服务器 | IP | 角色 |
| --- | --- | --- |
| 服务器1 | `45.134.82.177` | StarNexus 业务节点，主后台任务节点 |
| 服务器2 | `45.134.82.157` | StarNexus 业务节点，备用业务节点 |
| 服务器3 | `45.134.82.172` | PostgreSQL + Redis 数据库节点 |

访问链路：

```text
用户
  |
Cloudflare / Cloudflare Tunnel
  |
服务器1 业务节点       服务器2 业务节点       后续新增业务节点
  |                     |                     |
  +---------------------+---------------------+
                        |
              服务器3 PostgreSQL + Redis
```

## 核心规则

服务器3只跑 PostgreSQL 和 Redis，不跑 StarNexus 业务容器。

服务器1、服务器2只跑 StarNexus 业务容器，不再启动本机 PostgreSQL / Redis 容器。

生产环境只能有一台业务服务器使用：

```env
NODE_TYPE=master
```

其它业务服务器全部使用：

```env
NODE_TYPE=slave
```

每台业务服务器的 `NODE_NAME` 必须唯一。

所有生产业务服务器的 `SESSION_SECRET` 和 `CRYPTO_SECRET` 必须完全一致。

## 业务服务器通用数据库配置

所有业务服务器都连接服务器3：

```env
SQL_DSN=postgresql://starNex:<POSTGRES_PASSWORD>@45.134.82.172:5432/starnex
REDIS_CONN_STRING=redis://:<REDIS_PASSWORD>@45.134.82.172:6379/1
```

注意：

- `starnex` 是数据库名，小写。
- `starNex` 是数据库用户名，注意大小写。
- 真实密码只放在 `.env` 文件里，不要写进文档。
- `.env.prod3` 是独立测试栈，会启动自己的 `postgres-staging` / `redis-staging` 容器；它的 Redis 密码可以和生产服务器3不同，不作为生产服务器3密码来源。

## 服务器1配置

服务器1是主业务节点：

```env
NODE_TYPE=master
NODE_NAME=xingyuapi-prod-1
```

部署命令：

```bash
cp .env.prod .env
docker compose -f docker-compose.prod.yml up -d --build
```

## 服务器2配置

服务器2是备用业务节点：

```env
NODE_TYPE=slave
NODE_NAME=xingyuapi-prod-2
```

部署命令：

```bash
cp .env.prod2 .env
docker compose -f docker-compose.prod2.yml up -d --build
```

## 新增业务服务器流程

假设新增一台业务服务器，IP 是 `<NEW_APP_SERVER_IP>`。

1. 把 StarNexus 项目部署到新服务器。

2. 创建 `.env`，数据库和 Redis 继续指向服务器3：

```env
SQL_DSN=postgresql://starNex:<POSTGRES_PASSWORD>@45.134.82.172:5432/starnex
REDIS_CONN_STRING=redis://:<REDIS_PASSWORD>@45.134.82.172:6379/1
```

3. 设置节点类型和唯一节点名：

```env
NODE_TYPE=slave
NODE_NAME=xingyuapi-prod-3
```

如果再新增服务器，就继续递增：

```text
xingyuapi-prod-4
xingyuapi-prod-5
...
```

4. 确认新服务器的 `SESSION_SECRET` 和 `CRYPTO_SECRET` 与服务器1/2完全一致。

5. 在服务器3放行新业务服务器访问 PostgreSQL 和 Redis：

```bash
ufw allow from <NEW_APP_SERVER_IP> to any port 5432 proto tcp
ufw allow from <NEW_APP_SERVER_IP> to any port 6379 proto tcp
ufw reload
ufw status
```

6. 如果服务器3还使用宝塔安全组或云厂商安全组，也要添加：

```text
5432/tcp 来源 <NEW_APP_SERVER_IP>
6379/tcp 来源 <NEW_APP_SERVER_IP>
```

7. 如果 PostgreSQL 使用 `pg_hba.conf` 控制来源，在服务器3添加：

```conf
host    all    all    <NEW_APP_SERVER_IP>/32    md5
```

然后重启 PostgreSQL。

8. 从新业务服务器测试连通性：

```bash
nc -vz 45.134.82.172 5432
nc -vz 45.134.82.172 6379
psql -h 45.134.82.172 -p 5432 -U starNex -d starnex
redis-cli -h 45.134.82.172 -p 6379 -a '<REDIS_PASSWORD>' -n 1 ping
```

预期结果：

```text
5432 succeeded
6379 succeeded
PostgreSQL 可以登录
Redis 返回 PONG
```

9. 启动业务容器：

```bash
docker compose -f docker-compose.prod.yml up -d --build
docker compose logs -f
```

## 服务器3检查清单

服务器3上 PostgreSQL 和 Redis 应该监听在服务器3地址上，并且只允许业务服务器访问。

检查监听：

```bash
ss -tulpn | grep -E '5432|6379'
```

检查防火墙：

```bash
ufw status
```

不要把数据库端口全网开放：

```text
不要开放 5432/tcp 来源 0.0.0.0/0
不要开放 6379/tcp 来源 0.0.0.0/0
```

## 常用排查命令

业务服务器测试服务器3端口：

```bash
nc -vz 45.134.82.172 5432
nc -vz 45.134.82.172 6379
```

测试 PostgreSQL：

```bash
psql -h 45.134.82.172 -p 5432 -U starNex -d starnex
```

测试 Redis：

```bash
redis-cli -h 45.134.82.172 -p 6379 -a '<REDIS_PASSWORD>' -n 1 ping
```

查看业务容器：

```bash
docker ps
docker compose logs --tail 100
```

## sub2api 迁移到服务器3

sub2api 和 StarNexus 共用服务器3的 PostgreSQL + Redis，但必须使用独立数据库和独立 Redis DB。

推荐分配：

```text
StarNexus PostgreSQL: starnex
StarNexus Redis DB:   1
sub2api PostgreSQL:   sub2api
sub2api Redis DB:     2
```

服务器3创建 sub2api 数据库和用户：

```sql
CREATE USER sub2api WITH PASSWORD '<SUB2API_DB_PASSWORD>';
CREATE DATABASE sub2api OWNER sub2api;
GRANT ALL PRIVILEGES ON DATABASE sub2api TO sub2api;
```

服务器1导出现有 sub2api PostgreSQL：

```bash
docker exec sub2api-postgres sh -lc 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f /tmp/sub2api.dump'
docker cp sub2api-postgres:/tmp/sub2api.dump /root/sub2api.dump
scp /root/sub2api.dump root@45.134.82.172:/root/sub2api.dump
```

服务器3恢复 sub2api 数据库：

```bash
/www/server/pgsql/bin/pg_restore -h 127.0.0.1 -p 5432 -U sub2api -d sub2api --clean --if-exists --no-owner /root/sub2api.dump
```

服务器1的 `deploy/sub2api/.env` 设置为连接服务器3：

```env
DATABASE_HOST=45.134.82.172
DATABASE_PORT=5432
POSTGRES_USER=sub2api
POSTGRES_PASSWORD=<SUB2API_DB_PASSWORD>
POSTGRES_DB=sub2api

REDIS_HOST=45.134.82.172
REDIS_PORT=6379
REDIS_PASSWORD=<REDIS_PASSWORD>
REDIS_DB=2
```

然后只重建/重启 sub2api 应用，不再启动本机 `sub2api-postgres` 和 `sub2api-redis`：

```bash
cd /www/wwwroot/starnexus/deploy/sub2api
docker compose -f docker-compose.local.yml up -d sub2api
docker logs -f sub2api
```

确认 sub2api 正常后，旧的本机 `sub2api-postgres` / `sub2api-redis` 先停止保留，不要立刻删除：

```bash
docker stop sub2api-postgres sub2api-redis
```

稳定运行一段时间后，再考虑备份并删除旧容器和旧数据目录。

## 安全备注

数据库密码、Redis 密码、Cloudflare Tunnel token 不要写入文档或聊天记录。

如果密码或 token 曾经发到聊天、日志、截图里，正式生产前应轮换。
