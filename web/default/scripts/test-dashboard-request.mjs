import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import { QueryClient } from '@tanstack/react-query'
import ts from 'typescript'

const source = await readFile(new URL('../src/lib/dashboard-request.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source.replace(/^import .+$/gm, ''), {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 },
}).outputText

async function setup(responses) {
  const calls = []
  const messages = []
  const delays = []
  const dependencies = {
    api: { get: async (...args) => {
      calls.push(args)
      const next = responses.shift()
      if (next instanceof Error) throw next
      assert.ok(next, 'unexpected extra request')
      return { data: next }
    } },
    toast: { error: (message) => messages.push(message) },
    i18next: { t: (message) => message },
    setTimeout: (callback, ms) => { delays.push(ms); callback() },
  }
  const module = await import(`data:text/javascript;base64,${Buffer.from(
    `export function bind({api, toast, i18next, setTimeout}) {\n${compiled.replaceAll('export ', '')}\nreturn {getDashboardResponse, DashboardRequestError}\n}`
  ).toString('base64')}`)
  return { ...module.bind(dependencies), calls, messages, delays }
}

test('busy responses retry with backoff without transient error toasts', async () => {
  const ctx = await setup([
    { success: false, code: 'dashboard_busy' },
    { success: false, message: 'query dashboard data: dashboard aggregate is busy' },
    { success: true, data: { quota: 42 } },
  ])
  const response = await ctx.getDashboardResponse('/api/data', { start_timestamp: 100 })
  assert.equal(response.data.quota, 42)
  assert.equal(ctx.calls.length, 3)
  assert.equal(ctx.messages.length, 0)
  assert.equal(ctx.calls[0][1].skipBusinessError, true)
  assert.ok(ctx.delays[0] >= 500 && ctx.delays[0] < 800)
  assert.ok(ctx.delays[1] >= 1000 && ctx.delays[1] < 1300)
})

test('sustained overload stops after three requests and preserves cached data', async () => {
  const ctx = await setup(Array.from({ length: 3 }, () => ({ success: false, code: 'dashboard_busy' })))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key = ['statistics']
  client.setQueryData(key, { quota: 42 })
  await assert.rejects(client.fetchQuery({
    queryKey: key,
    queryFn: () => ctx.getDashboardResponse('/api/log/stat'),
  }), ctx.DashboardRequestError)
  assert.deepEqual(client.getQueryData(key), { quota: 42 })
  assert.equal(ctx.calls.length, 3)
  assert.equal(ctx.messages.length, 1)
  client.clear()
})

test('validation failures and transport failures are not retried by the helper', async () => {
  const validation = await setup([{ success: false, message: 'invalid time range' }])
  await assert.rejects(validation.getDashboardResponse('/api/log'), /invalid time range/)
  assert.equal(validation.calls.length, 1)
  assert.deepEqual(validation.messages, ['invalid time range'])
  const transport = await setup([new Error('network unavailable')])
  await assert.rejects(transport.getDashboardResponse('/api/log'), /network unavailable/)
  assert.equal(transport.calls.length, 1)
  assert.equal(transport.messages.length, 0)
})
