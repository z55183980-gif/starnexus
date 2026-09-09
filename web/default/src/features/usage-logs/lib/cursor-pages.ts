import type { GetLogsParams } from '../types'

type CursorWindow = {
  cursors: Map<number, number>
  start: number
  end: number
  expires: number
}

// Store only IDs and range boundaries, never log contents. Separate filters,
// page sizes and authenticated users cannot reuse each other's cursors.
export class LogCursorPages {
  private windows = new Map<string, CursorWindow>()

  prepare(identity: number, params: GetLogsParams, now = Date.now()) {
    const page = params.p || 1
    const size = params.page_size || 20
    const { p: _page, before_id: _cursor, ...filters } = params
    const key = JSON.stringify([
      identity,
      size,
      Object.entries(filters)
        .filter(([, value]) => value !== undefined)
        .sort(([a], [b]) => a.localeCompare(b)),
    ])
    let window = this.windows.get(key)
    if (!window || window.expires <= now || page === 1) {
      const end = params.end_timestamp || Math.floor(now / 1000)
      window = {
        cursors: new Map(),
        start: params.start_timestamp || end - 45 * 86400,
        end,
        expires: now + 30 * 60 * 1000,
      }
      if (this.windows.size >= 64) {
        const oldest = this.windows.keys().next().value
        if (oldest !== undefined) this.windows.delete(oldest)
      }
      this.windows.set(key, window)
    }
    return {
      key,
      window,
      page,
      params: {
        ...params,
        p: page,
        page_size: size,
        start_timestamp: window.start,
        end_timestamp: window.end,
        before_id: params.before_id ?? window.cursors.get(page),
      },
    }
  }

  remember(request: ReturnType<LogCursorPages['prepare']>, nextCursor: number) {
    // Ignore late responses from a filter refresh or a replaced page-one query.
    if (this.windows.get(request.key) !== request.window || nextCursor <= 0)
      return
    if (request.window.cursors.size >= 2048) {
      const oldest = request.window.cursors.keys().next().value
      if (oldest !== undefined) request.window.cursors.delete(oldest)
    }
    request.window.cursors.set(request.page + 1, nextCursor)
  }
}
