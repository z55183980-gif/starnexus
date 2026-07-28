# Local Upstream Account Pool Design

## 1. Purpose

StarNexus keeps channels as the user-facing logical routing unit and adds a local
upstream account pool as an optional channel credential source. Accounts are not
materialized as channels, and account pools are not StarNexus user groups.

The initial supported platform is OpenAI:

- Codex OAuth accounts using the ChatGPT Codex Responses endpoint.
- OpenAI API key accounts using the configured OpenAI-compatible base URL.
- HTTP/SSE upstream transport for local Codex pools. Client WebSocket and SSE
  transports remain independent from the selected upstream transport.

The source behavior baseline is sub2api v0.1.165. The implementation migrates
the account, scheduling, and proxy semantics that StarNexus needs; it does not
copy sub2api user, order, billing, redemption, or historical usage subsystems.

## 2. Existing Architecture and Integration Boundary

The current request path selects a channel in `middleware/distributor.go`, writes
the channel key and settings to the Gin context, and then invokes the relay. HTTP
relay retries may select another channel in `controller/relay.go`. Responses
WebSocket also performs channel selection for each turn in
`controller/responses_websocket.go`.

The target path is:

```text
client transport (WebSocket or HTTP/SSE)
  -> existing channel scheduler
  -> selected channel
  -> AccountRouter when credential_source=local_account_pool
  -> account credential + resolved proxy + concurrency lease
  -> existing provider adaptor over the selected upstream transport
```

The AccountRouter is a service-layer component. Middleware and controllers only
coordinate acquisition, context attachment, result reporting, retry, and release.
Database access stays in the model layer.

## 3. Channel Contract

Channels gain these persisted fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `credential_source` | string | `channel_key`, `channel_multi_key`, or `local_account_pool` |
| `upstream_account_pool_id` | nullable integer | Referenced local pool when the source is `local_account_pool` |

Compatibility rules:

- Existing rows are backfilled as `channel_key`; existing multi-key JSON remains
  managed by the current channel multi-key implementation.
- A local-pool channel must reference an active, compatible pool and may have an
  empty `channels.key`.
- Channel create/update validation rejects a pool reference with a different
  platform or credential capability.
- Changing away from a local pool clears `upstream_account_pool_id` but never
  deletes the pool or its accounts.
- Copying a channel copies the pool reference, not account credentials.
- Channel key reveal APIs never expose local pool credentials.

## 4. Data Model

All JSON documents are stored as `TEXT` and encoded through `common/json.go`.
GORM migrations and queries must work on SQLite, MySQL 5.7.8+, and PostgreSQL
9.6+.

### 4.1 `upstream_account_pools`

| Field | Notes |
| --- | --- |
| `id`, `name`, `description` | Administrative identity |
| `platform` | Initially `openai` |
| `credential_type` | `mixed`, `oauth`, or `apikey` |
| `status` | `active` or `inactive` |
| `default_proxy_id` | Optional pool proxy |
| `scheduler_config` | TEXT JSON with versioned scheduler settings |
| `source_system`, `source_id` | Idempotent migration identity |
| `created_at`, `updated_at` | Unix timestamps consistent with StarNexus models |

The scheduler configuration initially supports `version=1` and `top_k` (0 means
unlimited, otherwise 1-100). Unknown versions and invalid values fail pool
validation rather than silently changing scheduling behavior. Additional score
signals may be added in a later version without changing the version 1 contract.

### 4.2 `upstream_accounts`

| Category | Fields |
| --- | --- |
| Identity | `id`, `name`, `notes`, `platform`, `type` |
| Encrypted credential | `credential_ciphertext`, `credential_nonce`, `credential_key_version`, `credential_version` |
| Provider metadata | `extra` TEXT JSON |
| Proxy | `proxy_id`, `proxy_fallback_origin_id` |
| Scheduling | `concurrency`, `priority`, `weight`, `load_factor`, `rate_multiplier`, `status`, `schedulable` |
| Runtime state | `error_message`, `last_used_at`, `rate_limited_at`, `rate_limit_reset_at`, `overload_until`, `temp_unschedulable_until`, `temp_unschedulable_reason` |
| Usage window | `session_window_start`, `session_window_end`, `session_window_status` |
| Expiry | `expires_at`, `auto_pause_on_expired` |
| Migration | `source_system`, `source_id` |
| Audit | `created_at`, `updated_at` |

Supported account types are `oauth` and `apikey`. Credential payloads are
provider-specific versioned JSON. OAuth credentials include access token,
refresh token, account ID, token expiry, and the fields required by the current
Codex adaptor. API key credentials include the API key and optional base URL or
organization values.

The public/admin DTO returns credential presence, version, expiry, and masked
identity only. Plaintext credentials are accepted on write and decrypted only
inside the execution, refresh, test, and migration boundaries.

### 4.3 `upstream_account_pool_members`

| Field | Notes |
| --- | --- |
| `pool_id`, `account_id` | Composite primary key |
| `priority`, `weight` | Pool-specific scheduling override |
| `created_at` | Membership audit time |

An account may belong to multiple pools. Its concurrency is global across all
pools; membership priority and weight are local to each pool.

### 4.4 `upstream_proxies`

| Field | Notes |
| --- | --- |
| Identity | `id`, `name`, `protocol`, `host`, `port`, `status` |
| Encrypted auth | ciphertext, nonce, key version, credential version |
| Expiry | `expires_at`, `expiry_warn_days` |
| Fallback | `fallback_mode` (`none`, `proxy`, `direct`), `backup_proxy_id` |
| Health | last test time, latency, status, message, observed IP/location metadata |
| Migration | `source_system`, `source_id` |
| Audit | `created_at`, `updated_at` |

Supported protocols are `http`, `https`, `socks5`, and `socks5h`.

Deletion is rejected when the proxy is referenced by an account, pool, channel,
or another proxy's backup chain. A force operation is not provided in the first
version; administrators must reassign references explicitly.

### 4.5 `upstream_account_events`

Events contain account, pool, proxy, channel, request, and lease identifiers when
available, plus event type, result, redacted message, metadata TEXT JSON, and
timestamp. Credentials, authorization headers, proxy passwords, and raw upstream
bodies are forbidden.

## 5. Credential Encryption and Rotation

Credentials use AES-256-GCM with a fresh random 96-bit nonce per write.

Environment configuration:

- `UPSTREAM_ACCOUNT_CREDENTIAL_KEYS`: JSON object mapping positive key versions
  to base64-encoded 32-byte keys.
- `UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION`: active version used for new writes.

Additional authenticated data binds ciphertext to record kind, record ID,
credential version, and key version. A local-pool channel fails closed when the
active keyring is missing or malformed.

Rotation procedure:

1. Add the new key while retaining old keys.
2. Make the new version active.
3. Re-encrypt records in bounded batches using compare-and-update on credential
   version.
4. Verify that no record references the retired version.
5. Remove the old key from the environment.

The migration importer encrypts credentials before inserting them. Plaintext is
never written to a staging table or migration log.

## 6. Account Eligibility and Scheduling

An account is eligible only when all of these are true:

- membership and pool are active;
- account is active and schedulable;
- OAuth/API key credential is decryptable and structurally valid;
- expiry has not passed when auto-pause is enabled;
- rate-limit reset, overload, and temporary-unschedulable windows have elapsed;
- requested platform, model, endpoint capability, and upstream transport match;
- the global account concurrency lease can be acquired;
- the resolved proxy is usable or a configured fallback can be resolved.

The scheduler first keeps the smallest pool-member/account priority tier, which
matches sub2api's priority direction. Within that tier, optional `top_k` keeps
the least-loaded candidates and weighted random ordering uses the pool-member or
account weight multiplied by available capacity. This avoids deterministic
hot-spotting while keeping the first implementation observable and testable.

The scheduler reports deterministic exclusion counts such as `inactive`,
`not_schedulable`, `rate_limited`, `expired`, `concurrency_full`,
`proxy_unavailable`, `model_unsupported`, and `credential_invalid`. A no-account
error includes these counts without exposing account secrets.

## 7. Concurrency Lease

The account's `concurrency` value is a global limit across all pools and channels.
Each acquisition creates a random lease ID.

Redis mode uses an atomic script that:

- removes expired lease entries;
- rejects acquisition when the active count reaches the account limit;
- stores the lease with an expiry score and refreshable TTL;
- releases only the matching lease ID.

Long-running streams refresh the lease periodically. Release is idempotent.
Normal HTTP/SSE requests release after the relay finishes. Responses WebSocket
acquires and releases once per turn, not once per client connection. Retry always
releases the previous account before selecting another account or channel.

Without Redis, the current implementation uses a process-local lease manager.
That is suitable only for a single StarNexus instance; multi-instance deployments
must provide Redis because process-local leases cannot enforce a global limit.

## 8. Proxy Resolution and Fallback

Proxy precedence is:

```text
account proxy -> pool default proxy -> channel proxy -> direct
```

The first configured level is authoritative. If that proxy is expired or
unhealthy, its fallback chain is followed:

- `proxy`: traverse `backup_proxy_id` until a usable proxy is found;
- `direct`: use a direct connection;
- `none`: mark this account attempt unavailable.

Cycles, missing backup rows, inactive backups, and exhausted chains are rejected
deterministically. Fallback resolution never mutates the account's configured
proxy during request handling. Administrative expiry processing may perform an
explicit audited reassignment.

## 9. Retry and Error Ownership

Errors are classified by their narrowest owner:

| Owner | Examples | Action |
| --- | --- | --- |
| Account | invalid/expired token, account 401, account-specific 429 or quota | update account state, release, try another account |
| Proxy | connect/TLS/DNS failure attributable to selected proxy | record proxy health, follow fallback or try another account |
| Pool | no eligible accounts, invalid scheduler config, credential keyring unavailable | fail the local-pool channel attempt |
| Channel | unsupported model mapping, invalid base URL, provider-wide rejection | use existing channel retry/disable policy |
| Client/request | invalid request, cancellation, unsupported input | return without penalizing account or channel |

Account retries are bounded by unique attempted account IDs and the request
retry budget. A retry must not charge twice, emit duplicate client-visible SSE
events, or carry a previous account's key/proxy/lease in context.

## 10. OAuth Lifecycle

Account OAuth reuses the current Codex PKCE, token exchange, usage lookup, and
refresh primitives but stores credentials in `upstream_accounts`.

Required operations:

- start authorization and create a short-lived server-side state record;
- complete authorization and create or update an account;
- reauthorize an existing account;
- manually refresh one or many accounts;
- automatically refresh candidates before expiry;
- atomically rotate access and refresh tokens by credential version;
- clear recoverable auth errors after a successful refresh/test.

The worker is controlled by `UPSTREAM_ACCOUNT_REFRESH_WORKER_ENABLED` and is not
tied to `NODE_TYPE=master`. Each account refresh uses a Redis lock with owner
token and TTL; unlock uses compare-and-delete. A stale refresh result cannot
overwrite a newer credential version.

## 11. API Surface

All routes require root authorization because they expose infrastructure and
credential operations.

- `/api/upstream/account-pools`: list, get, create, update, delete, list members,
  and replace members. Pool responses include current active-member count and
  rolling 24-hour request statistics.
- `/api/upstream/accounts`: list, get, create, update, delete, batch
  create/update/delete, test, OAuth start/complete, and manual refresh.
- `/api/upstream/proxies`: list, get, create, update, delete, batch
  create/update/delete, and connectivity test.

sub2api migration is intentionally an offline CLI workflow. Idempotency is
provided by `(source_system, source_id)` and the import run identifier rather
than a public migration endpoint.

## 12. Frontend Information Architecture

The admin sidebar adds:

- Account Management: account table, filters, bulk actions, account editor,
  OAuth flow, account tests, and nested account pool management.
- IP Management: proxy table, bulk import, tests, fallback chain, references.

Channel create/edit adds a credential-source selector. Selecting local account
pool replaces the key editor with an active compatible pool selector and a link
to pool management. It does not enable upstream WebSocket automatically.

All visible strings use i18next keys and are synchronized for English, Chinese,
French, Russian, Japanese, and Vietnamese.

## 13. sub2api v0.1.165 Field Mapping

The importer reads sub2api without mutating it.

| sub2api | StarNexus | Rule |
| --- | --- | --- |
| `groups` used by OpenAI accounts | `upstream_account_pools` | Import only referenced OpenAI groups and scheduling settings |
| `accounts` | `upstream_accounts` | Import OpenAI `oauth` and `apikey`; preserve runtime scheduling fields |
| `account_groups` | `upstream_account_pool_members` | Preserve membership and priority; default weight 1 |
| `proxies` | `upstream_proxies` | Import referenced proxies plus reachable backup chain |
| `accounts.credentials` | encrypted credential fields | Normalize then encrypt before insert |
| `accounts.extra` | `extra` TEXT | Preserve recognized runtime/capability metadata and unknown keys |
| OpenAI scheduler settings | pool `scheduler_config` | Store a versioned normalized subset |

Idempotency uses `(source_system, source_id)`. Validation compares source and
target identity counts, mapped operational fields, membership edges and
priorities, proxy references, and decrypted credential equality without
printing credential values.

## 14. Migration and Test-Environment Cleanup

Migration phases:

1. Read-only discovery and preview with counts; invalid referenced data fails the plan.
2. Import proxies and backup chains.
3. Import pools.
4. Import encrypted accounts.
5. Import memberships and validate references.
6. After verification, explicitly select an imported local account pool in the
   StarNexus channel editor; the migration CLI does not modify channels.
7. Run account/proxy tests on S2.

Individual destination records and membership reconciliations are transactional
and the full import is resumable by source identity. A failure can leave a
partially imported but internally committed target state; rerunning the same
import repairs it. Existing StarNexus channel keys remain unchanged.

The migration CLI provides plan, import, verify, and run-scoped rollback modes.
Rollback deletes only rows carrying the matching import run ID and only when
they are not referenced by non-imported channels or records. S2 data is
disposable, so reverse export, backup, and rollback rehearsal are not deployment
gates; a failed S2 migration may be cleaned up or rebuilt directly.

## 15. Verification Gates

Before S2 deployment:

- model and migration tests pass on SQLite, MySQL, and PostgreSQL;
- encryption, wrong-key, tamper, rotation, and redaction tests pass;
- eligibility, priority, weighted selection, global concurrency, lease expiry,
  retry release, and proxy fallback tests pass;
- HTTP/SSE and Responses WebSocket turn tests prove the selected credential and
  proxy are scoped correctly and leases are released;
- frontend typecheck, unit tests, i18n synchronization, and production build pass.

On S2:

- preview and two repeated imports produce the same target state; S2 may be
  cleared and re-imported directly when a test run fails;
- migrated accounts refresh and test successfully without secret logs;
- account and proxy failures switch within the pool before channel failover;
- concurrent long streams do not exceed account limits or leak leases;
- Codex WebSocket and HTTP/SSE clients both work while local Codex upstream stays
  on HTTP/SSE;
- long realistic prompts are used for first client-visible SSE latency comparison;
- latency values are non-zero when output is observed and match the established
  first client-visible event definition.

S1, S3, and production databases remain read-only until every gate passes and a
separate production rollout is authorized.
