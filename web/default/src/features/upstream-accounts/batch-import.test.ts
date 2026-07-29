import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  extractImportItems,
  mergeAccountImportDocuments,
  normalizeAccountImportItem,
} from './batch-import'

describe('extractImportItems', () => {
  test('accepts a plain JSON array', () => {
    const items = [{ name: 'first' }, { name: 'second' }]

    assert.deepEqual(extractImportItems(items, 'accounts'), items)
  })

  test('extracts accounts from an exported data object', () => {
    const accounts = [{ name: 'account-1' }]

    assert.deepEqual(
      extractImportItems({ accounts, proxies: [] }, 'accounts'),
      accounts
    )
  })

  test('extracts proxies from an exported data object', () => {
    const proxies = [{ name: 'proxy-1' }]

    assert.deepEqual(
      extractImportItems({ accounts: [], proxies }, 'proxies'),
      proxies
    )
  })

  test('rejects an export without the requested collection', () => {
    assert.equal(extractImportItems({ accounts: [] }, 'proxies'), null)
  })

  test('normalizes a legacy Sub2API OpenAI OAuth account', () => {
    const account = {
      name: 'legacy-openai',
      platform: 'openai',
      type: 'oauth',
      credentials: {
        access_token: 'access-token',
        refresh_token: 'refresh-token',
        chatgpt_account_id: 'account-42',
      },
      extra: { privacy_mode: 'training_off' },
      concurrency: 0,
      priority: 0,
      rate_multiplier: 0,
      auto_pause_on_expired: false,
    }

    assert.deepEqual(normalizeAccountImportItem(account), {
      ...account,
      credentials: {
        ...account.credentials,
        account_id: 'account-42',
      },
      extra: JSON.stringify(account.extra),
    })
  })

  test('preserves a current Sub2API account_id', () => {
    const normalized = normalizeAccountImportItem({
      name: 'current-openai',
      platform: 'OpenAI',
      type: 'OAuth',
      credentials: {
        access_token: 'access-token',
        account_id: 'current-id',
        chatgpt_account_id: 'legacy-id',
      },
      extra: {},
    }) as Record<string, unknown>

    assert.equal(
      (normalized.credentials as Record<string, unknown>).account_id,
      'current-id'
    )
    assert.equal(normalized.extra, '{}')
  })

  test('keeps a StarNexus export item importable', () => {
    const account = {
      name: 'exported-account',
      platform: 'openai',
      type: 'apikey',
      credentials: { api_key: 'secret' },
      extra: '{"responses_mode":"auto"}',
      concurrency: 1,
      priority: 50,
      weight: 1,
      status: 'active',
      schedulable: true,
      auto_pause_on_expired: true,
      oauth_refresh_owner: 'external',
      pool_ids: [],
    }

    assert.deepEqual(
      extractImportItems(
        {
          type: 'sub2api-data',
          version: 1,
          proxies: [],
          accounts: [account],
        },
        'accounts'
      ),
      [account]
    )
  })

  test('merges complete Sub2API data documents', () => {
    const merged = mergeAccountImportDocuments([
      {
        type: 'sub2api-data',
        version: 1,
        proxies: [{ proxy_key: 'http|host|80||' }],
        accounts: [
          {
            name: 'oauth',
            platform: 'openai',
            type: 'oauth',
            credentials: {
              access_token: 'token',
              chatgpt_account_id: 'account-id',
            },
            extra: {},
          },
        ],
      },
    ])

    assert.equal(merged.type, 'sub2api-data')
    assert.equal(merged.proxies.length, 1)
    assert.equal(merged.accounts.length, 1)
    assert.equal(
      (
        (merged.accounts[0] as Record<string, unknown>)
          .credentials as Record<string, unknown>
      ).account_id,
      'account-id'
    )
  })
})
