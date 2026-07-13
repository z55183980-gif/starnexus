const NODE_ORIGINS = {
  s1: 'origin-s1.dkby.com',
  s2: 'origin-s2.dkby.com',
  s3: 'origin-s3.dkby.com',
}

const ADMIN_PATH = '/__dkby/node-routing/sync'

export default {
  async fetch(request, env) {
    const url = new URL(request.url)
    if (url.pathname === ADMIN_PATH) {
      return handleAdminSync(request, env)
    }

    const token = extractToken(request, url)
    if (!token) {
      return fetchDefault(request, env)
    }

    const tokenHash = await sha256(normalizeToken(token))
    const node = await env.USER_NODE_BINDINGS.get(`token:${tokenHash}`)
    const targetHost = NODE_ORIGINS[node]
    if (!targetHost) {
      return fetchDefault(request, env)
    }

    url.hostname = targetHost
    const routedRequest = new Request(url.toString(), request)
    routedRequest.headers.set('X-DKBY-Routed-Node', node)
    return fetch(routedRequest)
  },
}

function fetchDefault(request, env) {
  const defaultOrigin = String(env.DEFAULT_ORIGIN || '').trim()
  if (!defaultOrigin) {
    return fetch(request)
  }
  const url = new URL(request.url)
  url.hostname = defaultOrigin
  return fetch(new Request(url.toString(), request))
}

async function handleAdminSync(request, env) {
  if (request.method !== 'POST') {
    return jsonResponse({ success: false, message: 'method not allowed' }, 405)
  }
  const auth = request.headers.get('authorization') || ''
  const expected = `Bearer ${env.NODE_ROUTER_ADMIN_SECRET || ''}`
  if (!env.NODE_ROUTER_ADMIN_SECRET || !(await secureEqual(auth, expected))) {
    return jsonResponse({ success: false, message: 'unauthorized' }, 401)
  }

  let payload
  try {
    payload = await request.json()
  } catch {
    return jsonResponse({ success: false, message: 'invalid json' }, 400)
  }
  const userId = Number(payload.user_id)
  const node = String(payload.node || '').toLowerCase()
  const tokenHashes = Array.isArray(payload.token_hashes)
    ? [...new Set(payload.token_hashes.map(String))]
    : []
  if (
    !Number.isInteger(userId) ||
    userId <= 0 ||
    !['auto', 's1', 's2', 's3'].includes(node)
  ) {
    return jsonResponse({ success: false, message: 'invalid binding' }, 400)
  }
  if (tokenHashes.some((hash) => !/^[a-f0-9]{64}$/.test(hash))) {
    return jsonResponse({ success: false, message: 'invalid token hash' }, 400)
  }

  const userKey = `user:${userId}`
  const previous =
    (await env.USER_NODE_BINDINGS.get(userKey, { type: 'json' })) || {}
  const previousHashes = Array.isArray(previous.token_hashes)
    ? previous.token_hashes
    : []
  const currentSet = new Set(tokenHashes)

  for (const hash of previousHashes) {
    if (node === 'auto' || !currentSet.has(hash)) {
      await env.USER_NODE_BINDINGS.delete(`token:${hash}`)
    }
  }
  if (node === 'auto') {
    for (const hash of tokenHashes) {
      await env.USER_NODE_BINDINGS.delete(`token:${hash}`)
    }
    await env.USER_NODE_BINDINGS.delete(userKey)
  } else {
    for (const hash of tokenHashes) {
      await env.USER_NODE_BINDINGS.put(`token:${hash}`, node)
    }
    await env.USER_NODE_BINDINGS.put(
      userKey,
      JSON.stringify({ node, token_hashes: tokenHashes })
    )
  }

  return jsonResponse({
    success: true,
    user_id: userId,
    node,
    token_count: tokenHashes.length,
  })
}

function extractToken(request, url) {
  let token = request.headers.get('authorization') || ''
  const protocols = request.headers.get('sec-websocket-protocol') || ''
  if (protocols) {
    for (const part of protocols.split(',')) {
      const value = part.trim()
      if (value.startsWith('openai-insecure-api-key')) {
        token = value.replace(/^openai-insecure-api-key\.?/, '')
        break
      }
    }
  }

  if (
    url.pathname.includes('/v1/messages') ||
    url.pathname.includes('/v1/models')
  ) {
    token = request.headers.get('x-api-key') || token
  }
  if (
    url.pathname.startsWith('/v1beta/models') ||
    url.pathname.startsWith('/v1beta/openai/models') ||
    url.pathname.startsWith('/v1/models/')
  ) {
    token = url.searchParams.get('key') || token
    token = request.headers.get('x-goog-api-key') || token
  }

  const bearer = token.match(/^Bearer\s+(.+)$/i)
  if (bearer) token = bearer[1]
  if (!token || token === 'midjourney-proxy') {
    token = request.headers.get('mj-api-secret') || ''
    const mjBearer = token.match(/^Bearer\s+(.+)$/i)
    if (mjBearer) token = mjBearer[1]
  }
  return token
}

function normalizeToken(token) {
  const normalized = token.trim().replace(/^sk-/, '')
  return normalized.split('-')[0]
}

async function sha256(value) {
  const digest = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(value)
  )
  return Array.from(new Uint8Array(digest))
    .map((byte) => byte.toString(16).padStart(2, '0'))
    .join('')
}

async function secureEqual(left, right) {
  const [a, b] = await Promise.all([sha256(left), sha256(right)])
  if (a.length !== b.length) return false
  let diff = 0
  for (let i = 0; i < a.length; i += 1) {
    diff |= a.charCodeAt(i) ^ b.charCodeAt(i)
  }
  return diff === 0
}

function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8' },
  })
}
