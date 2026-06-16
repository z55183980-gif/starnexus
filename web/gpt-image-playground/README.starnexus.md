# GPT Image Playground (embedded)

This directory contains the upstream [GPT Image Playground](https://github.com/CookSleep/gpt_image_playground) source, vendored inside StarNexus.

## Role in StarNexus

- Served as static assets at `/image-playground/`
- Opened from the sidebar menu **生图工作台** (`/image-workbench`)
- Calls the same-origin StarNexus relay at `/v1/images/*`

## Development

From repo root:

```bash
cd web/gpt-image-playground
npm install
npm run dev
```

For StarNexus integration defaults, copy `.env.starnexus.example` to `.env.local` before building.

## Sync into default frontend

From `web/default`:

```bash
npm run image-playground:sync
```

This builds this package and copies `dist/` to `web/default/public/image-playground/`.

Production Docker and `make build-frontend` run this step automatically.

## Upgrade upstream

Replace files under `web/gpt-image-playground/` with a newer upstream release, then run `npm run image-playground:sync` and verify `/image-workbench`.
