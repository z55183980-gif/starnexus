# StarNexus Key Tool download entry

## Latest update: fixed download entries and docs hosting

The website now uses the four fixed entries documented in [the download runbook](../deploy/downloads/README.md). Primary downloads redirect to immutable files in the docs site's existing R2 bucket; GitHub remains available through separate backup entries. The version badge was removed so future installer version changes do not require a frontend rebuild. Both public mirrors and actual browser-initiated downloads matched the hashes in `deploy/downloads/starnexus-keytool.json`; HTTP range downloads returned 206 with the expected bytes.

- Runtime source: `24b21bbb099b6a9f0ee23802a6f64dd32de29eb1`.
- Successful build: https://github.com/z55183980-gif/starnexus/actions/runs/43407323343.
- Image: `ghcr.io/z55183980-gif/starnexus@sha256:68e831893f5e0d4064bb2ec5db40b0a9ce84be78a0c3a702e813ef98d870de6c`.
- Current web container: `starnexus-keytool-web-24b21bbb0`, localhost port 3004. Previous port 3003 remains available for old static chunks and rollback.
- Business API port 3001 and desktop-login port 3002 remain unchanged, with their original containers and start times preserved. The existing desktop-login HTML route also remains on port 3002.
- Nginx loads only four exact-match redirect locations from `/www/wwwroot/starnexus/keytool-downloads/starnexus-keytool.conf`. These return 302/no-store and can be changed independently of the frontend. The corresponding manifest is stored alongside it.
- Web deployment evidence and the previous Nginx configuration are in `/www/wwwroot/starnexus/keytool-web-24b21bbb0/`. Restoring that release's `nginx.before.conf` rolls the web UI back to port 3003 while retaining fixed download entries; verify no newer routing edits exist before restoring a full backup.
- Static fallback order is current web 3004, previous web 3003, business 3001, then desktop-login 3002. No application restart was required for the switch.
- Typecheck, focused lint/format, production build and browser checks passed. Browser tests used mocked account/API responses and real public HTML, static files and installer downloads. Checks covered 320/390/1440 widths, Chinese/English text, both primary downloads, both backup URLs, menu keyboard operation, and opening the create-key dialog without submitting.

The following sections record the original deployment, before the fixed-entry update.

Deployed on 2026-11-13 (Asia/Singapore) to https://dkby.com/keys.

The API key page now uses the approved StarNexus logo button, a blue/indigo background, a visible download label and separate Windows/macOS menu entries. The Mac entry identifies Apple Silicon/Intel support and Apple notarization. The action group wraps on small screens; the create-key dialog remains available. The original copyright notices and other project identities are unchanged.

## Installers

Both links are pinned to the independent StarNexus Key Tool 0.1.2 release:

- Windows: https://github.com/z55183980-gif/starnexus-keytool/releases/download/v0.1.2/StarNexus-Key-Tool_Windows_x64-setup.exe
- macOS: https://github.com/z55183980-gif/starnexus-keytool/releases/download/v0.1.2/StarNexus-Key-Tool_macOS_universal.dmg

Public, unauthenticated downloads were fetched completely and matched the previously built installers:

| File | Bytes | SHA256 |
| --- | ---: | --- |
| Windows EXE | 4372565 | `a4fedf0f381629506d470b7722669f6be82e2595374544fcef7bb60bc20a112e` |
| Mac DMG | 8659922 | `79cc43f1ebba16dbd64d2e11e05e11c63d6d2b3d55fdc0b3d62c235cf90499c6` |

Mac is Developer ID signed, Apple notarized and stapled. Windows is not code-signed. The release repository contains the Mac verification record. No zAI repository, release, signing secret or application identity was changed.

## Production isolation

- Source branch: `codex/keytool-download-0.1.2`, based on the existing production desktop-login branch, without unrelated edits from the main working directory.
- Runtime source: `a4e4354dae06ad9aa624e8445278501d3028b39c`.
- Build: https://github.com/z55183980-gif/starnexus/actions/runs/43404822957 (successful).
- Image: `ghcr.io/z55183980-gif/starnexus@sha256:d5041ee6bbce335d77d7761b419a3873875afcd76a7698e8ebd6b11e5a3c1ae5`.
- Current S1 (`S1`, 144.34.138.219), new web instance: `starnexus-keytool-web-a4e4354da`, localhost port 3003.
- Existing business instance `xingyuapi-new-api` remains on port 3001, with its original image and start time.
- Existing desktop-login instance `starnexus-desktop-auth-8fb01bd73` remains on port 3002, with its original image and start time.
- The existing business and desktop-auth APIs retain their original Nginx upstreams. Only known web routes use port 3003. `/desktop-login` and the three `/api/desktop/*` routes retain port 3002.
- `/static/` uses the new web instance, with 404 fallback to the original business and then desktop-login instances, preserving assets used by already-open tabs.
- No database, channel, model, DNS, original business Compose, S2 or zAI configuration was changed.
- The workflow's floating `main` image tag is now conditional on running on `main`; dispatching this branch publishes only its SHA tag.

The isolated checkout also restores `web/default/src/features/usage-logs/data/schema.ts`, an existing local schema accidentally excluded by the broad `data/` ignore rule. Its consumers import it as a type; restoring it makes the clean-checkout typecheck pass without changing their runtime behavior.

## Validation

- TypeScript check, focused ESLint and Prettier checks passed; production frontend and Docker image builds passed.
- Real frontend browser checks passed at widths 1440, 390 and 320, in Chinese and English. Checks include logo load, menu bounds, both download URLs, Enter/Escape operation and opening the create-key dialog without submitting it.
- The same checks passed against public production HTML/static resources using mocked account/API responses. No real user keys, accounts or sessions were used. The site currently forces its light theme, including when the browser prefers dark.
- Public downloads match the installer hashes above.
- Six legacy entry assets were fetched through Nginx and matched their original bytes.
- `/api/status` and `/api/desktop/config` succeeded; original containers remained healthy, with unchanged images and start times. Observation verifies business API requests still use port 3001 and desktop APIs still use port 3002. Existing business errors can still occur; this does not claim an error-free service.

Server-only evidence and rollback material: `/www/wwwroot/starnexus/keytool-web-a4e4354da/`. `compose.web.json` contains inherited credentials and must remain mode 600; do not copy it into Git or logs. `baseline.json`, `verification.json`, `activated-at`, `web-paths.json`, `nginx.before.conf` and `nginx.after.conf` record the deployment.

The initial immediate post-reload check hit the old Nginx worker and automatically restored the prior configuration. A bounded readiness check then confirmed the new worker before completing the switch; no verification step was bypassed.

## Rollback

After confirming there have been no later routing edits, restore the server-only `nginx.before.conf` to `/etc/nginx/nginx.conf`, run `nginx -t`, and gracefully reload Nginx. If later edits exist, remove only this release's web route entries and static fallback locations instead. This returns web traffic to the existing instance without restarting either original application. Keep port 3003 available for rollback inspection until no requests use it. Do not consolidate API traffic onto the web or desktop-login instance.
