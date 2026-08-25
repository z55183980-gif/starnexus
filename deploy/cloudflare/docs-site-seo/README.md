# docs.dkby.com 第一阶段 SEO 部署

`docs.dkby.com` 当前由独立的 Cloudflare R2 静态站点提供，不经过本项目的 Go Web 路由。因此本目录中的公开文件需要上传到 `docs` 桶的 `docs/` 前缀（与现有 `docs/help.html`、`docs/doc/` 对齐）：

- `robots.txt`：`Content-Type: text/plain; charset=utf-8`
- `sitemap.xml`：`Content-Type: application/xml; charset=utf-8`
- `llms.txt`：`Content-Type: text/plain; charset=utf-8`
- `llms-full.txt`：`Content-Type: text/plain; charset=utf-8`
- `doubao-seedance-video.html`：上传为 `docs/doc/doubao-seedance-video.html`，`Content-Type: text/html; charset=utf-8`
- `help.html`：文档目录外壳；为频繁更新的 iframe 文档附加修订号，上传为 `docs/help.html`

上传后清除这四个 URL 的 Cloudflare 缓存，并逐项确认：

1. `https://docs.dkby.com/robots.txt` 返回 200 和纯文本，不能返回 HTML。
2. `https://docs.dkby.com/sitemap.xml` 返回 200 和合法 XML。
3. `https://docs.dkby.com/llms.txt` 返回 200 和纯文本。
4. `https://docs.dkby.com/llms-full.txt` 返回 200 和纯文本，且可直接检索到 API Key、Codex、OpenClaw、Hermes Agent、CC Switch 和 Seedance 等主题。
5. Cloudflare 的 **AI Crawl Control / Content Signals Policy** 允许搜索与回答生成爬虫（如 OAI-SearchBot、ChatGPT-User），训练类爬虫保持禁止。
6. R2 自定义域名未开启“所有未知路径回退到首页”；不存在的文档地址应返回 404。

如果存储桶开启了 Cloudflare 托管 `robots.txt`，Cloudflare 可能会在源站内容前追加规则。上线后必须检查最终响应，确保追加内容没有禁止 OAI-SearchBot、ChatGPT-User、Claude-SearchBot、Claude-User、PerplexityBot 或 Perplexity-User。
