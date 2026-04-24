// QR v2 primitive — create a pending login session and long-poll for confirmation.
// Backend: internal/adapter/handler/qr_handler.go
// Both endpoints are public (IP-rate-limited); no auth required.

const BASE = '/api/v2/qr'

/**
 * Create a new QR login session.
 * @returns {Promise<{id: string, action: string, qr_payload: string, expires_in: number}>}
 */
export async function createQRSession() {
  const res = await fetch(`${BASE}/session`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action: 'login' }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
  return data
}

/**
 * Long-poll the session status. Server holds up to `timeout` seconds (max 30).
 * Returns early on state change.
 *
 * Shape:
 *   pending   → {status:'pending'}
 *   confirmed → {status:'confirmed', action:'login', token, expires_in}
 *   consumed  → throws Error('session_consumed')     (410)
 *   expired   → throws Error('session_not_found')    (404)
 *
 * @param {string} id
 * @param {number} timeoutSec
 * @param {AbortSignal} [signal]
 */
export async function pollQRStatus(id, timeoutSec = 25, signal) {
  const res = await fetch(
    `${BASE}/${encodeURIComponent(id)}/status?timeout=${timeoutSec}`,
    { signal },
  )
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const err = new Error(data.error || `HTTP ${res.status}`)
    err.status = res.status
    throw err
  }
  return data
}
