// ZLogin API — proxies Zitadel Session API v2 through the lurus-identity backend.
// Used exclusively by the /zlogin custom login page for OIDC flows.

const BASE = '/api/v1'

async function post(path, body) {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error(data.error || `HTTP ${res.status}`)
  }
  return res.json()
}

async function get(path) {
  const res = await fetch(BASE + path)
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error(data.error || `HTTP ${res.status}`)
  }
  return res.json()
}

/**
 * Fetch info about the OIDC auth request (which app is requesting login).
 * @param {string} authRequestId
 * @returns {Promise<{appName?: string, [key: string]: any}>}
 */
export function getAuthInfo(authRequestId) {
  return get(`/auth/info?authRequestId=${encodeURIComponent(authRequestId)}`)
}

/**
 * Submit email + password, create a Zitadel session, complete the OIDC callback.
 * @param {string} authRequestId
 * @param {string} username  - email or login name
 * @param {string} password
 * @returns {Promise<{callback_url: string}>}
 */
export function submitPassword(authRequestId, username, password) {
  return post('/auth/zlogin/password', {
    auth_request_id: authRequestId,
    username,
    password,
  })
}

/**
 * After WeChat login, link the lurus session to the Zitadel OIDC flow and get the callback URL.
 * @param {string} authRequestId
 * @param {string} lurusToken  - the lurus_token from WeChat login callback
 * @returns {Promise<{callback_url: string}>}
 */
export function linkWechatAndComplete(authRequestId, lurusToken) {
  return post('/auth/wechat/link-oidc', {
    auth_request_id: authRequestId,
    lurus_token: lurusToken,
  })
}
