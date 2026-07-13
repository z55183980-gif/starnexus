# User node router

Deploy `user-node-router.js` as a Cloudflare Worker in front of `api.dkby.com/*`.

Required bindings and secrets:

- KV binding: `USER_NODE_BINDINGS`
- Worker secret: `NODE_ROUTER_ADMIN_SECRET`
- Optional Worker variable: `DEFAULT_ORIGIN`

Cloudflare dashboard values:

1. Create a KV namespace, for example `dkby-user-node-bindings`.
2. In the Worker's Bindings page, add a KV namespace binding whose variable name is exactly `USER_NODE_BINDINGS`, then select that namespace.
3. In Variables and Secrets, add the secret name `NODE_ROUTER_ADMIN_SECRET`; its value is a strong random string shared only with the application servers.
4. Leave `DEFAULT_ORIGIN` absent initially. Add it as a plain-text variable with a value such as `origin-s1.dkby.com` only when a fixed automatic fallback is required.
5. Attach the Worker route `api.dkby.com/*`.

Leave `DEFAULT_ORIGIN` empty to preserve the current `api.dkby.com` DNS/load-balancer path. If the load balancer is retired later, set it to a live fallback such as `origin-s1.dkby.com` before removing the load balancer.

Keep the proxied DNS record for `api.dkby.com` and the Worker route active even after retiring the load balancer. `DEFAULT_ORIGIN` replaces only the automatic users' upstream target; it does not replace the public Worker hostname.

Application environment variables on every backend node:

```env
NODE_ROUTER_ADMIN_URL=https://<worker-name>.<workers-subdomain>.workers.dev/__dkby/node-routing/sync
NODE_ROUTER_ADMIN_SECRET=replace-with-the-same-strong-secret
```

Using the direct `workers.dev` management URL keeps KV synchronization independent of the public load-balancer route. The `https://api.dkby.com/__dkby/node-routing/sync` address also works after the Worker route is attached.

The Worker normalizes API tokens in the same way as the gateway (`sk-` prefix and optional suffix removed), hashes the normalized token with SHA-256, and stores only the hash in KV.

Route only `api.dkby.com/*`. Do not attach the Worker to `*.dkby.com/*`, because node-specific origin requests must not re-enter the Worker.

Deploy the matching application version to every backend node before enabling user binding. Older nodes do not expose the binding API and do not refresh KV when users add or delete API keys.

KV updates are eventually consistent across Cloudflare locations, so a binding change may take a short time to become visible everywhere.
