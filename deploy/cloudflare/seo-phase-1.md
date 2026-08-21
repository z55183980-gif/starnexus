# 星域互联 SEO / GEO 第一阶段上线清单

本阶段目标是让搜索引擎、AI 搜索产品和潜在用户更准确地理解星域互联（StarNexus）的产品定位、适用人群、使用方式和权威信息来源。`llms.txt` 是补充发现入口，不替代可抓取正文、正确状态码、站点地图和持续内容建设。

## 1. 部署主站

1. 构建并部署当前后端与 `web/default` 前端产物。
2. 确认生产配置中的 `ServerAddress` 为 `https://dkby.com`。如需显式覆盖规范域名，可设置：

   ```text
   SEO_CANONICAL_ORIGIN=https://dkby.com
   ```

3. 清除 Cloudflare 对主站 HTML 与以下机器可读入口的缓存：
   - `https://dkby.com/robots.txt`
   - `https://dkby.com/sitemap.xml`
   - `https://dkby.com/llms.txt`
   - `https://dkby.com/llms-full.txt`
   - `https://dkby.com/index.md`
4. Cloudflare 中保留 HTTP 到 HTTPS、`www.dkby.com` 到 `dkby.com` 的边缘 301 规则。应用层已经提供兜底规范化跳转，但边缘跳转能减少一次回源。

## 2. 部署文档站机器可读文件

按照 [`docs-site-seo/README.md`](./docs-site-seo/README.md) 将该目录中的 4 个公开文件上传到 `docs.dkby.com` 对应 R2 `docs/` 前缀，并清除相应缓存。

## 3. 上线后验收

使用浏览器或 `curl` 检查：

```text
https://dkby.com/
https://dkby.com/robots.txt
https://dkby.com/sitemap.xml
https://dkby.com/llms.txt
https://dkby.com/llms-full.txt
https://dkby.com/index.md
https://docs.dkby.com/robots.txt
https://docs.dkby.com/sitemap.xml
https://docs.dkby.com/llms.txt
https://docs.dkby.com/llms-full.txt
```

验收标准：

- 主站和文档页均返回正确的 `200`；规范域名跳转返回 `301`。
- 不存在的主站路径返回 `404`，私有控制台页面带 `noindex`。
- XML 站点地图使用 `application/xml`；Markdown/文本入口不是 SPA HTML。
- 首页 HTML 源码中包含产品正文、规范链接、Open Graph 和 JSON-LD，而不是只有空壳节点。
- 价格、模型排行等动态页面只有在公开可访问时才进入 sitemap；实时信息仍以站内页面/API 为准。

## 4. 提交搜索平台

1. 在 Google Search Console 验证 `dkby.com` 与 `docs.dkby.com`，分别提交对应 `sitemap.xml`。
2. 在 Bing Webmaster Tools 验证两个站点并提交 sitemap；Bing 数据可同时覆盖其搜索生态中的发现入口。
3. 使用 URL 检查工具请求抓取首页、文档帮助页和新发布的核心指南；不要批量请求无价值页面。
4. 在 Cloudflare AI Crawl Control 中确认 `OAI-SearchBot`、`ChatGPT-User`、`Claude-SearchBot`、`Claude-User`、`PerplexityBot` 与 `Perplexity-User` 未被边缘规则误拦截。

## 5. 第一阶段指标基线

上线当天记录以下基线，之后每周观察一次：

- Google/Bing 已收录页面数与抓取错误。
- 品牌词“星域互联”“StarNexus”“dkby”曝光、点击和平均位置。
- 非品牌词带来的自然搜索访问与注册转化。
- 首页、价格页和核心文档的搜索落地访问量。
- AI 搜索来源的引荐访问，以及这些访问的注册/创建令牌转化率。

不要把“是否存在 `llms.txt`”作为成功指标。第一阶段真正的成功标准是：公开页面能被稳定抓取、产品实体和用途没有歧义、搜索曝光开始增长，并且自然访问能进入注册与使用流程。
