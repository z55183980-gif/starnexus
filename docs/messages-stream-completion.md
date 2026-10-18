# Messages stream completion and content timing

Anthropic Messages streams complete when `message_stop` has been written and
flushed successfully. A downstream cancellation waits for the in-flight frame
handler to finish recording this result. Cancellation before the terminal frame
and failed writes remain errors. Cleanup closes the upstream body before waiting
for its scanner, including when protocol completion precedes HTTP EOF.

For native Messages responses, usage-log `frt` now measures the first successfully
forwarded nonempty text or tool event. Pings, message headers, thinking, signatures
and usage-only events do not count as answer content. If no content was observed,
`frt` is null and `frt_source` is `content_unobserved`; otherwise the source is
`downstream_content`. Valid upstream generation timing is retained separately as
`generation_first_token_ms`. This describes server-side write/flush, not a client
receipt acknowledgement.

Transport `FirstResponseTime` and its retry guard remain unchanged. Heartbeats
still reach clients and count as connection activity. This change does not alter
request conversion, prompts, tools, history, cache controls, account scheduling,
usage projection, pricing or replay policy. Other response protocols retain their
existing display timing. No channel setting or database migration is required.

Regression checks cover delayed upstream EOF, cancellation during terminal
flush, early cancellation, downstream write failure, heartbeat-before-content,
unknown content timing, and preservation of cache usage including one-hour writes.

Deployment verification must correlate new test request IDs with usage logs:
both EOF readers and clients closing immediately after message_stop must report
normal completion. A client closing before message_stop must remain a failure.
Historical usage rows are not rewritten.
