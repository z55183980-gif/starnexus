# Responses Lite tool history compatibility

Responses Lite normalization preserves existing `input.additional_tools` items
in their original order. Namespace changes across history are forwarded to the
upstream; the gateway does not treat a namespace as a globally unique tool
definition or merge/overwrite its historical children.

Supported request-level `tools` are moved as a separate developer
`additional_tools` prefix. They never modify an existing declaration. Migration
removes the root field only after the entire prefix is encoded successfully.
Unknown tool types or shapes retain the original tool fields and continue to
the upstream. JSON numbers and extension fields remain lossless.

Non-tool protocol requirements (`reasoning.context`, `store`, serialized
instructions, `parallel_tool_calls`, and encrypted reasoning inclusion) keep
their existing policies. This does not bypass authentication, request limits,
or upstream validation, and does not add retries.

Fallback diagnostics contain the request ID supplied by the logger, a bounded
reason code, and the number of existing tool declarations. Tool schemas and
user content are not logged.

For an emergency migration rollback, set `RESPONSES_LITE_TOOLS_PASSTHROUGH=true`
on the S2 service. This preserves both root tools and input without re-enabling
the old history-conflict rejection. Non-tool normalization still runs. Upstream
acceptance of unconverted root tools depends on that endpoint.

Tests cover incremental namespaces, reordered children, changed schemas,
preserved item positions, mixed top-level/history tools, exact integer values,
unknown shapes, idempotence, and atomic failure. Transport tests send the
prepared payloads to a mock upstream through HTTP, passthrough, WebSocket
preparation, and HTTP bridge preparation, checking that an upstream 400 remains
an upstream response and does not cause an extra request.

Protocol reference:
https://developers.openai.com/api/docs/guides/tools-tool-search#add-tools-at-a-specific-point-in-the-input
