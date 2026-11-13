# StarNexus Key Tool download entry

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
