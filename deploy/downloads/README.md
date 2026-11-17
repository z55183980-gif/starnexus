# StarNexus fixed installer downloads

The API key page uses stable URLs. Targets are maintained independently of the frontend build.

| Fixed entry | Destination |
| --- | --- |
| `https://dkby.com/downloads/starnexus-keytool/windows` | Windows installer on docs.dkby.com |
| `https://dkby.com/downloads/starnexus-keytool/macos` | Notarized universal Mac DMG on docs.dkby.com |
| `https://dkby.com/downloads/starnexus-keytool/windows/github` | Windows GitHub backup |
| `https://dkby.com/downloads/starnexus-keytool/macos/github` | Mac GitHub backup |

The four exact-match Nginx locations return 302 with `Cache-Control: no-store, max-age=0`. They neither invoke application APIs nor forward user query parameters. Installers are immutable, versioned objects in the existing Cloudflare R2 `docs` bucket under `docs/downloads/starnexus-keytool/<version>/`. Objects carry download filenames, SHA256 metadata and long-lived cache headers. Existing documents and the old configuration tools are preserved.

## Change installer version without rebuilding the website

1. Upload the original signed installers to a **new version directory**; do not overwrite a versioned file. Keep the matching GitHub release as the backup.
2. Fetch the public files and verify full byte counts, SHA256, attachment filenames and range requests. This preserves the Mac signature and notarization because the file bytes do not change.
3. Update `starnexus-keytool.json` with version, file names, sizes, hashes and both destinations.
4. Run `python deploy/downloads/render-nginx.py` from the repository root. It validates target hosts and renders only the four download routes.
5. On current production S1, back up `/www/wwwroot/starnexus/keytool-downloads/starnexus-keytool.conf` and its JSON manifest. Upload the generated include to a staging path, then replace the include. Run `nginx -t` and gracefully `systemctl reload nginx`; if validation fails, restore the saved include before reloading. Do not replace the main Nginx configuration or restart any application container.
6. Check all four **public** fixed entries: 302, the expected Location, `no-store`, and the primary file's complete hash after following the redirect. Restore the previous include if validation fails.

The include is loaded inside the existing S1 server block. It is not a standalone server configuration. No changes to API upstreams, desktop login routes, database, DNS or zAI are needed for installer updates.

The frontend deliberately does not hardcode the current release version. Platform support and signing labels still require a UI change if those properties change in a future release.
