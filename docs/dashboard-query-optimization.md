# Dashboard query behavior

## Interactive log browsing

The default frontend sends `include_total=false` to the administrator log API.
The API reads at most `page_size + 1` matching records, returns `has_more` and
`next_cursor`, and omits `total`. It does not execute a count query or acquire
an aggregate slot. Filters, access checks, the 45-day query range limit and the
existing deep-offset limit remain in force. No historical records are removed.

The two administrator log views use first/previous/next navigation and display
the current page without claiming a total page count. Existing classic clients
and callers that omit `include_total=false` retain the original total-count
contract. Deploy the backend before the frontend. A new frontend also accepts
the old backend's total-count response during rollout.

Full-range log statistics and usage-detail cost summaries are calculated only
after clicking **Calculate statistics**. Changing filters, turning pages or
focusing the window does not automatically run a full-range summary. Before a
successful calculation the view shows a dash, not zero. Explicit recalculation
can reuse the backend's existing short-lived cache.

The dashboard continues to read the existing `quota_data` aggregates and merge
pending usage; no new aggregation schema, billing formula or migration is
introduced. The business-monitor live-log requests also skip exact counts.

## Capacity and failure handling

Each existing pool retains its two active slots. Aggregate, quota-data and list
pools each allow at most eight additional waiting jobs, with a two-second wait
limit inside the existing 30-second operation budget. Queue saturation returns
the stable `dashboard_busy` code and `Retry-After: 1`, keeping HTTP 200 and the
`success: false` envelope for classic-client compatibility. Database failures
are not converted to successful empty responses.

The frontend retries busy responses at most twice with exponential delay and
jitter. Transient business failures do not produce a toast on every attempt.
Final business failures reject the query and preserve previously successful
data for that query key. The global query policy does not retry these already
handled errors again. Network errors continue through the existing HTTP error
handler. Old backend busy messages are recognized during rollout.

Existing stale-cache fallback is now limited to two minutes beyond the entry's
normal expiry, rather than lasting until eviction. It still requires the same
cache key: unrelated filters and time windows never share stale results.

## Validation and operational limits

- `go test ./controller ./model -count=1`: covers count-free browsing with all
  aggregate slots occupied, last-page boundaries, legacy totals, filters,
  queue saturation, cancellation, slot release, timeout and stale age limits.
- `node --test scripts/test-log-cursors.mjs scripts/test-dashboard-request.mjs`
  from `web/default`: covers cursor isolation, fixed ranges, busy retries,
  single final notification and preservation of cached data after failure.
- Frontend typecheck, changed-file lint/format and production build.

The limit remains per process, not per database cluster. This change does not
claim a measured production throughput improvement or introduce a report job
service. Before changing the active concurrency limit, measure database load,
query latency, cache hit rate and busy responses under the real workload.
Arbitrary-filter historical reports still use bounded on-demand SQL aggregation;
additional preaggregation or asynchronous reports require a separately defined
set of dimensions, retention and reporting requirements.
