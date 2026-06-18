# GPT Image Playground

基于 [CookSleep/gpt_image_playground](https://github.com/CookSleep/gpt_image_playground) 的 vendored 副本，作为 StarNexus monorepo 内的**独立前端子项目**维护。

## 独立开发（日常）

```bash
cd web/gpt-image-playground
npm install
npm run dev
```

- 不要长期使用 sync 脚本生成的 `.env.local`（其中的 `VITE_BASE=/image-playground/` 仅用于嵌入 StarNexus 构建）。
- 独立开发时无需 `.env.local`，或在设置页 / URL 参数中配置 API 地址与 Key。

### 对接 StarNexus 后端（可选）

1. 启动 Go 后端（默认 `http://localhost:3000`）。
2. 复制 `dev-proxy.config.example.json` 为 `dev-proxy.config.json` 并启用代理。
3. 在 Playground 设置中将 API Base 指向代理或后端地址。

### Mock API（无需后端）

```bash
npm run mock:api
```

### 测试

```bash
npm run test
npm run test:watch
```

## 嵌入 StarNexus（发布 / 集成验证时）

由 `scripts/sync-image-playground.mjs` 自动写入集成用 env 并构建，复制产物到 `web/default/public/image-playground/`：

```bash
cd web/default
npm run image-playground:sync
```

集成后：

- 静态资源路径：`/image-playground/`
- 控制台入口：侧边栏 **生图工作台** → `/image-workbench`
- API：同源 `/v1/images/*`

生产 Docker 与 `make build-frontend` 会自动执行 sync。

## 跟进上游

从 [上游仓库](https://github.com/CookSleep/gpt_image_playground) 获取新版本，合并到本目录后执行 `npm run image-playground:sync` 并在 `/image-workbench` 验证。

保留的 StarNexus 定制（合并时注意）：

- `vite.config.ts` 中的 `loadEnv` + `VITE_BASE` 支持
- `.env.starnexus.example`（sync 脚本参考）
