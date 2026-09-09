import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import ts from 'typescript'

const source = await readFile(
  new URL('../src/features/usage-logs/lib/cursor-pages.ts', import.meta.url),
  'utf8'
)
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 },
}).outputText
const { LogCursorPages } = await import(
  `data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`
)

test('sequential pages use cursors and a fixed time window', () => {
  const pages = new LogCursorPages()
  const first = pages.prepare(1, { p: 1, page_size: 20 }, 1800000000000)
  pages.remember(first, 500)
  const second = pages.prepare(1, { p: 2, page_size: 20 }, 1800000100000)
  assert.equal(second.params.before_id, 500)
  assert.equal(second.params.end_timestamp, first.params.end_timestamp)
  assert.equal(second.params.end_timestamp - second.params.start_timestamp, 45 * 86400)
  pages.remember(second, 400)
  assert.equal(pages.prepare(1, { p: 3, page_size: 20 }, 1800000100000).params.before_id, 400)
  assert.equal(pages.prepare(1, { p: 2, page_size: 20 }, 1800000100000).params.before_id, 500)
})

test('filters, users and page sizes isolate cursors; unseen pages have no cursor', () => {
  const pages = new LogCursorPages()
  const first = pages.prepare(1, { p: 1, type: 2, page_size: 20 })
  pages.remember(first, 500)
  for (const [id, params] of [
    [2, { p: 2, type: 2, page_size: 20 }],
    [1, { p: 2, type: 5, page_size: 20 }],
    [1, { p: 2, type: 2, page_size: 50 }],
    [1, { p: 5, type: 2, page_size: 20 }],
  ]) assert.equal(pages.prepare(id, params).params.before_id, undefined)
})

test('refresh ignores late results and expiration resets cursors', () => {
  const pages = new LogCursorPages()
  const old = pages.prepare(1, { p: 1 }, 1800000000000)
  const fresh = pages.prepare(1, { p: 1 }, 1800000000001)
  pages.remember(old, 500)
  assert.equal(pages.prepare(1, { p: 2 }, 1800000000002).params.before_id, undefined)
  pages.remember(fresh, 600)
  assert.equal(pages.prepare(1, { p: 2 }, 1800000000003).params.before_id, 600)
  assert.equal(pages.prepare(1, { p: 2 }, 1800002000000).params.before_id, undefined)
})
