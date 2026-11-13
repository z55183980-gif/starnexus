# StarNexus Key Tool website login

The independent StarNexus Key Tool 0.1.1 uses `/desktop-login` to authorize a desktop session. Existing password, two-factor and website sign-in flows remain available. No zAI application identity or data directory is reused.

The app creates a random state and PKCE S256 challenge. After website login, `/api/desktop/authorize` returns a fixed `com.dkby.starnexus.keytool://auth/callback` link containing a short-lived code. The native backend exchanges that code and its private verifier at `/api/desktop/exchange`, retaining the resulting session cookie only in memory. Redis grants expire after two minutes and are atomically consumed once. The app expires pending requests after ten minutes. API keys and passwords do not appear in callback URLs.

The website authorization page reuses the normal `/sign-in` page in a popup. It detects the completed session and returns to the app; a visible return link remains available when a browser blocks automatic external-protocol navigation. Users must install or start the new app before using this flow and allow the browser to open it.

## Deployment

Deployed on 2026-11-13 to the **current S1 entry**, as established by the November 10 migration record in the main working directory. Older documentation describing S2 as the only target is historical.

- Production baseline: `ddbb4b62f547e7a64aa2dd06bd4867c481a146ef`.
- Deployed application revision: `8fb01bd73fd78db33afaf16a4087d04f0b37b623`.
- Image: `ghcr.io/z55183980-gif/starnexus@sha256:79a056f2ac367f594ae2aacaa3e5e93e534556c84b98465b7832b0224b386b1c`.
- Image build: https://github.com/z55183980-gif/starnexus/actions/runs/43379554967
- Current Compose: `/www/wwwroot/starnexus/compose.json`; project `starnexus-s1-desktop-auth-8fb01bd73`.
- New instance: `xingyuapi-new-api`, localhost port 3002. Nginx was gracefully reloaded at 06:42:53 UTC.
- Previous instance: `xingyuapi-new-api-prev-ddbb4b62f`, port 3001, retained to finish existing requests and support rollback.
- Server-only backups and observations: `/www/wwwroot/starnexus/desktop-auth-8fb01bd73/`. Compose backups contain secrets and must remain private with mode 600.

Existing environment, shared session configuration, mounts, database, Redis and slave role were preserved. No schema, channel, model, DNS or zAI changes were made. The image was built from an isolated production checkout; unrelated changes in the main working directory were not included. Branch-only image tagging changes were reverted after the build so merging this feature does not alter normal main-branch publication.

For rollback, first verify the previous instance is healthy, restore `nginx.before.conf` to `/etc/nginx/nginx.conf`, run `nginx -t`, then gracefully reload Nginx. Keep both instances running until requests drain. Restore the saved Compose only if making the rollback persistent; it references the previous project and must be reconciled with the retained container name before running Compose commands. Do not recreate shared dependencies.

## Validation

- Desktop-specific Go controller/router tests passed, including PKCE mismatch, expiration, replay and concurrent single-use consumption.
- Browser automation covered existing website sessions, popup sign-in, desktop callback handling, and preserved direct password login using test accounts/responses.
- Public config and HTML endpoints passed; unauthenticated authorization, invalid exchange and browser-origin exchange were rejected.
- Existing public status endpoint remained healthy. Production observation is stored server-side without credentials or raw customer requests.
- No real customer credentials were used for end-to-end login.
- The broader token/model test selection encountered `TestListModelsTokenLimitIncludesTieredBillingModel` panicking on an uninitialized database; the identical failure was reproduced on the unmodified production baseline.
