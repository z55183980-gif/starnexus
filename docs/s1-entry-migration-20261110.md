# StarNexus 新 S1 入口迁移记录

记录日期：2026-11-10（Asia/Singapore）。用户批准先迁移 StarNexus，S2 保留邮件服务；随后明确授权执行。

## 最新状态：已完成全部正式入口请求切换

用户随后要求 S2 减少业务流量，并确认“按当前请求结束，保持现在的平滑切换”。已删除 S1 Nginx 的 Responses 和管理 API 转发规则，使用 `nginx -t` 后平滑 reload：所有新的正式入口请求均交给 S1 `127.0.0.1:3000`。

切换时 S2 上正在执行的请求继续完成；已观测到 6 个旧 Responses 请求返回 200，旧 Nginx worker 随后自然退出。S1 初始验证中 27 个 Responses 请求返回 200，应用记录 28 条消费日志，未见批量写入失败。此方式只保留正在执行的请求，不保证旧多轮会话的后续请求继续命中 S2 内存；旧验证码或仅保存在 S2 内存中的续聊状态可能需要重新发起。

S2 邮件、应用和旧隧道保留用于邮件及回滚，不停止。受限兼容 SSH 服务仍保留，但新 Nginx 配置已无指向它的路由。`origin-s2.dkby.com` 直接访问仍可到 S2，不属于正式入口自动分流。下文兼容路由表和首次迁移验证描述为历史阶段，不代表当前配置。

完整切换前的 Nginx 备份：S1 `/www/wwwroot/starnexus/migration/nginx-before-all-s1.conf`。本地操作记录：`.codex-deploy/s1-migration-20261110/all-s1-cutover.json`。

## 当前实际拓扑

| 对象 | 地址或配置 | 职责 |
| --- | --- | --- |
| 新 S1，SSH `S1` | `144.34.138.219` | 正式公网入口及全部新业务请求，含 Responses、管理 API |
| S2，SSH `sn-s2` | `192.243.114.184` | 邮件服务、保留应用与旧入口用于回滚 |
| DB7，SSH `sn-db7` | `192.243.115.154` | 两台应用共用 PostgreSQL `starnex`、Redis `/1` |

新 S1 与历史 `sn-s1` / `starnexus-s1`（104.194.82.238）不是同一服务器。历史 `origin-s1.dkby.com` 未修改，不用于发布新 S1。

正式入口：`dkby.com` CNAME 已由旧 S2 Tunnel 改为新 S1 Tunnel；`api.dkby.com`、`gateway.dkby.com`、`www.dkby.com` 继续 CNAME 到 `dkby.com`。

- 新隧道：`starnexus-s1-20261110`，ID `2d08e805-6071-4721-bac1-dab3f2fe6b39`。
- 旧 S2 隧道：`a30df043-d3fe-4cab-8477-1febe23aebeb`，持续运行。
- 验证域名：`s1-new.dkby.com`。
- `origin-s2.dkby.com`、MX、`mail.dkby.com`、SPF 均保持原配置。
- 本次未启用 Worker 或旧 Cloudflare Load Balancer。

## 首次迁移时的兼容路由（已取消）

新隧道 → S1 Nginx `127.0.0.1:8080`：

| 请求路径 | 应用目标 |
| --- | --- |
| `/v1/responses` 及子路径，包括 WebSocket | S2，经 `127.0.0.1:13002` SSH 转发 |
| `/api/*`，但不含精确路径 `/api/status` | S2，保证验证码、登录、OAuth、管理流程状态一致 |
| 其他路径，包括 `/v1/messages`、`/v1/chat/completions`、`/v1/models`、页面和 `/api/status` | S1 `127.0.0.1:3000` |

这是**入口迁移及部分业务迁移**，不是所有应用请求均已转移到 S1。当前 S2 的 Responses 缓存 Redis 写入关闭，已观测到大量 `local_only / write_disabled`；不能直接将这部分请求改发 S1，也不能通过固定等待 30 分钟来保证所有持续续聊完成。进一步迁移需单独处理会话兼容。

S1→S2 使用专用 SSH 密钥，S2 授权仅允许来自 S1 IP、转发到 `127.0.0.1:3000`，不允许交互 shell。私钥不写入仓库。Nginx 关闭请求/响应缓冲及自动上游重试，支持 Upgrade，读取/发送超时 3600 秒。

## 部署配置

- S1 应用：`/www/wwwroot/starnexus/compose.json`，服务 `new-api`，容器 `xingyuapi-new-api`。
- 镜像：`ghcr.io/z55183980-gif/starnexus@sha256:e604ef7bc56fd0eef9ce27b39e37e0c9ddec1dd0267a6f3911812683e9ca243c`。
- 应用版本：`main-487cefc`；revision `487cefc63c527b07f8546bfea7229d883d958212`。
- `/new-api` SHA256：`c829bfc6489442cb03d4928b0ede2b56d3bbcabcde5df2368dead05535f84cea`，与 S2 一致。
- 配置从 S2 实际容器提取；共享 session、crypto 和上游凭据密钥，不从仓库历史 `.env` 复制。
- `NODE_TYPE=slave` 保持；新节点名 `xingyuapi-s1-20261110`，未改变后台任务角色。
- S1 应用退出等待 3600 秒，Compose 3610 秒。Tunnel 2026.9.1 的 grace period 上限为 3 分钟，已设 3 分钟、容器等待 190 秒；任何后续重启仍须先排空长请求。
- S1 Tunnel Compose：`/www/wwwroot/starnexus/migration/tunnel-compose.json`。
- S1 Nginx：`/etc/nginx/nginx.conf`；systemd `nginx`；描述符限制 65535。
- S1 兼容 SSH 服务：`starnexus-s2-compat.service`，开机启动并自动重连。
- Nginx 路由日志：`/var/log/nginx/starnexus-migration.log`，不记录查询参数和认证头。
- DB7 已增加新 S1 `/32` 对 5432/6379 的允许规则，并更新现有 `db7-fw.service` 持久化；没有重启数据库或清空当前防火墙规则。
- S2 `/data` 初始复制时仅约 8 KB，视频缓存目录为空；12 GB 历史日志留在 S2。

配置及 Tunnel token 文件为 root 600；不得打印或提交其内容。原 S2 容器配置快照在 S1 `migration/s2-container-before.json`，含凭据，按敏感文件保护。

## 验证与限制

- 四个正式域名 `/api/status` 均多次返回 HTTP 200、success=true。
- 新 S1 应用 healthy；新 Tunnel 四条连接正常；受限 SSH 通道和 Nginx active。
- 本地入口、验证域名、正式 `api.dkby.com` 共三次 Claude 流式测试均 HTTP 200，并收到正常终止事件。
- 三条测试消费日志写入共享 DB7，每条输入 8 token、输出 3 token、quota=500。
- 实际 Responses 经 S1→S2 返回 200；管理 API 正常转发；两台应用数据库连接均已观测。
- 观察到 Responses 503，已在 S2 日志核实为用户分组下 `gpt-6-luna` 无可用渠道。另有 `gpt-5.4-mini` 无可用渠道。未修改这些业务配置。
- Tunnel 后续出现少量客户端取消流（remote canceled），因此不能宣称观察期绝对零错误或所有用户请求无中断。
- S2 容器启动时间保持 `2026-11-01T07:28:46Z`，没有重建或停止；Postfix、Dovecot active。
- 未主动发出测试邮件、执行支付或使用用户登录会话；邮件 TCP 连通及服务运行状态已验证。
- 新 S1 尚未注册独立节点监控记录；历史 routing_nodes 的 s1 仍代表历史 origin，不能据其状态判断新 S1。

## 回滚与后续发布

回滚入口只需把 `dkby.com` CNAME 恢复为 `a30df043-d3fe-4cab-8477-1febe23aebeb.cfargotunnel.com`。保留 S1/S2 两台应用和隧道，等待已有请求完成；不回滚共享数据库，不立即停机。

本地操作脚本：`.codex-deploy/s1_migration_20261110.py`，回滚命令：

```powershell
python .codex-deploy/s1_migration_20261110.py rollback-entry
```

DNS 原始快照和验证记录：`.codex-deploy/s1-migration-20261110/`（本地忽略目录）。DB7 原规则及 unit 备份：`/root/s1-migration-20261110/`。

后续发布必须同时考虑 S1 普通请求和 S2 兼容请求的版本一致性；不得再按“只有 S2 生产”或“所有流量均在 S1”的假设发布。应先设计分流与排空，再更新任何一台承接请求的应用。
