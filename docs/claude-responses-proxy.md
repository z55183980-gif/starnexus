# Claude gateway Responses compatibility

For a type 14 Claude channel whose upstream implements OpenAI Responses (for
example Sub2API), enable **Enable Claude Responses proxy** in the channel editor.
The corresponding channel `settings` JSON field is:

```json
{"claude_responses_proxy_enabled": true}
```

Merge this field into existing settings. Do not replace unrelated settings.
The base URL must be the gateway root, without `/v1`. This capability defaults
off and is not supported for local account pools. Do not enable it for the
official Anthropic endpoint or a Messages-only wrapper.

With the capability enabled, `/v1/responses` uses the upstream gateway's
`/v1/responses` endpoint and Bearer authentication. Responses JSON/SSE and client
tool declarations are handled by the existing Responses pipeline; Sub2API owns
conversion to Anthropic and any Wrapper MCP bridge. Channel model mapping,
parameter policies, and usage settlement still apply. `/v1/messages` continues
using the Claude adaptor. `/v1/responses/compact` is not provided by this flag.

Codex clients should select the Responses wire API and point at StarNexus's
`/v1` base URL. Verification for this change used HTTP/SSE, with upstream
WebSocket support disabled. It does not certify native WebSocket or compact
support. The tested `claude-opus-5-5` upstream rejects forced `tool_choice`;
use its supported `auto` or `none` modes.

Stream completion is recognized after successfully writing and flushing
`message_stop` (Claude) or a terminal successful Responses event. Cancellation
must wait for an in-flight frame handler to finish updating its status. A
disconnect before completion remains a failure, and a failed terminal write
cannot mark the stream complete. Closing the upstream body before waiting for
the scanner also removes the previous five-second cleanup delay.

## Verification on 2026-10-18

- Synthetic `tool_use` and `end_turn` streams with delayed HTTP EOF and client
  cancellation during terminal flush reproduced `client_gone/context canceled`
  on the old code. They now complete as `done`. Early cancellation and failed
  writes remain failures. Terminal regression tests passed 25 repetitions.
- Desktop Codex engine `0.162.0-alpha.17.2` through an isolated full StarNexus
  instance to the real Sub2API/Wrapper returned `43` and `turn.completed`.
- A synthetic client tool call, client result submission, and final streamed
  answer succeeded. This verifies the protocol round trip, not unrestricted
  execution of Codex shell/MCP tools.
- Real Messages `end_turn` and `tool_use` requests both returned `message_stop`.
  All four streamed requests had local usage status `ok/done`. Sub2API usage
  records 7160–7164 correspond to the five-request verification window on
  account 8; these are test records, not customer incident records.
- Relay packages, the relevant channel controller test, frontend form tests
  (17), TypeScript, scoped ESLint, formatting, and production build passed.
  Full Go tests also encountered existing failures in `common`, `controller`,
  and `service`. Re-running those packages with the changed production Go files
  replaced by HEAD through a Go overlay reproduced the same failures.

The original customer records (5739915 and 5739918) had no captured raw SSE or
client-side trace. The synthetic regression demonstrates a matching failure
mode, not proof that both customer sessions received their terminal frames.
Their Python-urllib Messages path also differs from direct Codex Responses.

Deploy the tested code, then enable this capability only for gateway-backed
channels (channel 86 in this incident). Validate a new Codex request and its
usage status after deployment. To roll back the capability, disable the flag;
existing Messages routing remains available. The local verification did not
deploy production code or change production channel settings.
