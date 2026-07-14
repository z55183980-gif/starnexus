const LEGACY_NODE_ORIGINS = {
  s1: 'origin-s1.dkby.com',
  s2: 'origin-s2.dkby.com',
  s3: 'origin-s3.dkby.com',
  s4: 'origin-s4.dkby.com',
}

const ADMIN_PATH = '/__dkby/node-routing/sync'
const ROUTING_TOKEN_PREFIX = 'routing-token:'
const ROUTING_USER_PREFIX = 'routing-user:'
const ROUTING_USER_TOKENS_PREFIX = 'routing-user-tokens:'
const ROUTING_REVISION_PREFIX = 'routing-revision:'

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
    const route = await resolveRoute(env, tokenHash)
    if (!route) {
      debugLog(env, { token_hash: tokenHash.slice(0, 12), route: 'auto' })
      return fetchDefault(request, env)
    }

    debugLog(env, {
      token_hash: tokenHash.slice(0, 12),
      node: route.node,
      target: route.origin,
    })
    url.hostname = route.origin
    const routedRequest = new Request(url.toString(), request)
    routedRequest.headers.set('X-DKBY-Routed-Node', route.node)
    return fetch(routedRequest)
  },
}

async function resolveRoute(env, tokenHash) {
  const userIdValue = await env.USER_NODE_BINDINGS.get(
    `${ROUTING_TOKEN_PREFIX}${tokenHash}`
  )
  if (userIdValue !== null) {
    const userId = Number(userIdValue)
    if (!Number.isInteger(userId) || userId <= 0) {
      return null
    }
    const route = await env.USER_NODE_BINDINGS.get(
      `${ROUTING_USER_PREFIX}${userId}`,
      { type: 'json' }
    )
    if (!route || route.node === 'auto') {
      return null
    }
    const origin = normalizeAllowedOrigin(route.origin, env)
    if (!origin) {
      return null
    }
    return { node: String(route.node), origin }
  }

  const legacyNode = await env.USER_NODE_BINDINGS.get(`token:${tokenHash}`)
  const legacyOrigin = LEGACY_NODE_ORIGINS[legacyNode]
  if (!legacyOrigin) {
    return null
  }
  return { node: legacyNode, origin: legacyOrigin }
}

function fetchDefault(request, env) {
  const defaultOrigin = normalizeAllowedOrigin(env.DEFAULT_ORIGIN, env)
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

  if (Number(payload.version) === 2) {
    return handleV2AdminSync(payload, env)
  }
  return handleLegacyAdminSync(payload, env)
}

async function handleLegacyAdminSync(payload, env) {
  const userId = Number(payload.user_id)
  const node = String(payload.node || '').toLowerCase()
  const tokenHashes = normalizeTokenHashes(payload.token_hashes)
  if (
    !Number.isInteger(userId) ||
    userId <= 0 ||
    !Object.hasOwn(LEGACY_NODE_ORIGINS, node) && node !== 'auto'
  ) {
    return jsonResponse({ success: false, message: 'invalid binding' }, 400)
  }
  if (!tokenHashes) {
    return jsonResponse({ success: false, message: 'invalid token hash' }, 400)
  }

  const v2RevisionKey = `${ROUTING_REVISION_PREFIX}${userId}`
  const [v2Revision, v2Route, v2TokenIndex] = await Promise.all([
    env.USER_NODE_BINDINGS.get(v2RevisionKey),
    env.USER_NODE_BINDINGS.get(`${ROUTING_USER_PREFIX}${userId}`, {
      type: 'json',
    }),
    env.USER_NODE_BINDINGS.get(`${ROUTING_USER_TOKENS_PREFIX}${userId}`),
  ])
  const migrated =
    v2Revision !== null || v2Route !== null || v2TokenIndex !== null
  if (migrated) {
    const currentNode = String(v2Route?.node || 'auto').toLowerCase()
    if (node !== currentNode) {
      return jsonResponse(
        {
          success: false,
          message: 'user routing has migrated to v2; upgrade this backend',
        },
        409
      )
    }
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

  if (migrated) {
    await syncV2Tokens(env, userId, tokenHashes)
  }

  return jsonResponse({
    success: true,
    version: 1,
    user_id: userId,
    node,
    token_count: tokenHashes.length,
  })
}

async function handleV2AdminSync(payload, env) {
  const action = String(payload.action || '')
  const userId = Number(payload.user_id)
  if (!Number.isInteger(userId) || userId <= 0) {
    return jsonResponse({ success: false, message: 'invalid user id' }, 400)
  }

  switch (action) {
    case 'sync_tokens':
      return syncV2Tokens(env, userId, payload.token_hashes)
    case 'set_route':
      return setV2Route(env, userId, payload)
    case 'delete_user':
      return deleteV2User(env, userId)
    default:
      return jsonResponse({ success: false, message: 'invalid action' }, 400)
  }
}

async function syncV2Tokens(env, userId, inputHashes) {
  const tokenHashes = normalizeTokenHashes(inputHashes)
  if (!tokenHashes) {
    return jsonResponse({ success: false, message: 'invalid token hash' }, 400)
  }
  const indexKey = `${ROUTING_USER_TOKENS_PREFIX}${userId}`
  const previous = (await env.USER_NODE_BINDINGS.get(indexKey, { type: 'json' })) || []
  const previousHashes = Array.isArray(previous) ? previous : []
  const currentSet = new Set(tokenHashes)

  for (const hash of previousHashes) {
    if (!currentSet.has(hash)) {
      await env.USER_NODE_BINDINGS.delete(`${ROUTING_TOKEN_PREFIX}${hash}`)
    }
  }
  for (const hash of tokenHashes) {
    await env.USER_NODE_BINDINGS.put(
      `${ROUTING_TOKEN_PREFIX}${hash}`,
      String(userId)
    )
  }
  await env.USER_NODE_BINDINGS.put(indexKey, JSON.stringify(tokenHashes))

  return jsonResponse({
    success: true,
    version: 2,
    action: 'sync_tokens',
    user_id: userId,
    token_count: tokenHashes.length,
  })
}

async function setV2Route(env, userId, payload) {
  const node = String(payload.node || '').trim().toLowerCase()
  const revision = Number(payload.revision)
  if (!/^[a-z0-9][a-z0-9_-]{0,31}$/.test(node) || !Number.isSafeInteger(revision) || revision <= 0) {
    return jsonResponse({ success: false, message: 'invalid route' }, 400)
  }

  const revisionKey = `${ROUTING_REVISION_PREFIX}${userId}`
  const currentRevision = Number(
    (await env.USER_NODE_BINDINGS.get(revisionKey)) || 0
  )
  if (revision < currentRevision) {
    return jsonResponse({ success: false, message: 'stale revision' }, 409)
  }

  const routeKey = `${ROUTING_USER_PREFIX}${userId}`
  if (node === 'auto') {
    await env.USER_NODE_BINDINGS.delete(routeKey)
  } else {
    const origin = normalizeAllowedOrigin(payload.origin, env)
    if (!origin) {
      return jsonResponse({ success: false, message: 'invalid origin' }, 400)
    }
    await env.USER_NODE_BINDINGS.put(
      routeKey,
      JSON.stringify({ node, origin, revision })
    )
  }
  await env.USER_NODE_BINDINGS.put(revisionKey, String(revision))

  return jsonResponse({
    success: true,
    version: 2,
    action: 'set_route',
    user_id: userId,
    node,
    revision,
  })
}

async function deleteV2User(env, userId) {
  const indexKey = `${ROUTING_USER_TOKENS_PREFIX}${userId}`
  const hashes = (await env.USER_NODE_BINDINGS.get(indexKey, { type: 'json' })) || []
  if (Array.isArray(hashes)) {
    for (const hash of hashes) {
      await env.USER_NODE_BINDINGS.delete(`${ROUTING_TOKEN_PREFIX}${hash}`)
    }
  }
  await env.USER_NODE_BINDINGS.delete(indexKey)
  await env.USER_NODE_BINDINGS.delete(`${ROUTING_USER_PREFIX}${userId}`)
  await env.USER_NODE_BINDINGS.delete(`${ROUTING_REVISION_PREFIX}${userId}`)

  const legacyUserKey = `user:${userId}`
  const legacyUser = await env.USER_NODE_BINDINGS.get(legacyUserKey, {
    type: 'json',
  })
  if (legacyUser && Array.isArray(legacyUser.token_hashes)) {
    for (const hash of legacyUser.token_hashes) {
      if (/^[a-f0-9]{64}$/.test(String(hash))) {
        await env.USER_NODE_BINDINGS.delete(`token:${hash}`)
      }
    }
  }
  await env.USER_NODE_BINDINGS.delete(legacyUserKey)

  return jsonResponse({
    success: true,
    version: 2,
    action: 'delete_user',
    user_id: userId,
  })
}

function normalizeTokenHashes(value) {
  if (!Array.isArray(value)) {
    return null
  }
  const hashes = [...new Set(value.map((hash) => String(hash).toLowerCase()))]
  return hashes.every((hash) => /^[a-f0-9]{64}$/.test(hash)) ? hashes : null
}

function normalizeAllowedOrigin(value, env) {
  const origin = String(value || '').trim().toLowerCase()
  if (!origin || origin.includes('/') || origin.includes(':')) {
    return ''
  }
  const suffix = String(env.ALLOWED_ORIGIN_SUFFIX || '.dkby.com')
    .trim()
    .toLowerCase()
  if (!suffix.startsWith('.') || !origin.endsWith(suffix)) {
    return ''
  }
  return origin
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

function debugLog(env, data) {
  if (String(env.NODE_ROUTER_DEBUG || '').toLowerCase() === 'true') {
    console.log(JSON.stringify({ type: 'node-route', ...data }))
  }
}

function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8' },
  })
}
