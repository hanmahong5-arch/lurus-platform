import React, { useCallback, useEffect, useRef, useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'
import { Button, Spin, Typography } from '@douyinfe/semi-ui'
import { createQRSession, pollQRStatus } from '../../api/qr'

const { Text } = Typography

// ── Phase names used internally to drive the UI state machine ──────────────
// loading  — creating a session
// waiting  — showing QR, long-polling for confirmation
// expired  — 5-min TTL elapsed or server returned 404/410
// error    — unrecoverable problem (network, 5xx)
// success  — we got a token; parent component will navigate away next tick
//
// We keep a session refresh count so React can re-mount the <QRCodeSVG/> cleanly
// when the user clicks "刷新二维码".
const PHASE = { loading: 'loading', waiting: 'waiting', expired: 'expired', error: 'error', success: 'success' }

/**
 * Self-contained QR login widget. Creates a /api/v2/qr session on mount,
 * renders the payload as an SVG, long-polls the status endpoint, and calls
 * `onLogin(token)` when the server reports a confirmed login.
 *
 * Polling is cancelled via AbortController on unmount / refresh so we don't
 * leak long-poll connections when the user switches tabs or navigates away.
 */
export default function QRLoginPanel({ onLogin }) {
  const [phase, setPhase] = useState(PHASE.loading)
  const [payload, setPayload] = useState('')
  const [errMsg, setErrMsg] = useState('')
  const [nonce, setNonce] = useState(0)
  const abortRef = useRef(null)

  const start = useCallback(async () => {
    setPhase(PHASE.loading)
    setErrMsg('')
    try {
      const { id, qr_payload } = await createQRSession()
      setPayload(qr_payload)
      setPhase(PHASE.waiting)

      const controller = new AbortController()
      abortRef.current = controller

      while (!controller.signal.aborted) {
        let res
        try {
          res = await pollQRStatus(id, 25, controller.signal)
        } catch (err) {
          if (controller.signal.aborted) return
          if (err.status === 404 || err.status === 410) {
            setPhase(PHASE.expired)
            return
          }
          setErrMsg(err.message || '轮询失败')
          setPhase(PHASE.error)
          return
        }

        if (res.status === 'confirmed' && res.token) {
          setPhase(PHASE.success)
          onLogin(res.token)
          return
        }
        // status === 'pending' → loop
      }
    } catch (err) {
      setErrMsg(err.message || '创建二维码失败')
      setPhase(PHASE.error)
    }
  }, [onLogin])

  // (Re-)start on mount and whenever the user explicitly refreshes (nonce++).
  useEffect(() => {
    start()
    return () => {
      abortRef.current?.abort()
    }
  }, [start, nonce])

  const refresh = () => setNonce((n) => n + 1)

  return (
    <div style={{ textAlign: 'center', padding: '16px 0' }}>
      <div style={qrFrameStyle}>
        {phase === PHASE.loading && (
          <div style={overlayStyle}><Spin /></div>
        )}
        {phase === PHASE.waiting && payload && (
          <QRCodeSVG value={payload} size={200} level="M" />
        )}
        {phase === PHASE.success && (
          <div style={overlayStyle}>
            <Text style={{ color: '#07c160', fontSize: 15 }}>✓ 登录成功</Text>
          </div>
        )}
        {phase === PHASE.expired && (
          <div style={overlayStyle}>
            <Text type="tertiary">二维码已过期</Text>
            <Button
              size="small"
              type="primary"
              theme="light"
              style={{ marginTop: 8 }}
              onClick={refresh}
            >
              刷新二维码
            </Button>
          </div>
        )}
        {phase === PHASE.error && (
          <div style={overlayStyle}>
            <Text type="danger" size="small">{errMsg}</Text>
            <Button size="small" style={{ marginTop: 8 }} onClick={refresh}>
              重试
            </Button>
          </div>
        )}
      </div>

      <Text style={{ display: 'block', marginTop: 16 }} type="secondary" size="small">
        使用 <b>路途 APP</b> 扫码登录
      </Text>
      <Text type="tertiary" size="small" style={{ display: 'block', marginTop: 4 }}>
        打开 APP → 点击首页「扫一扫」→ 扫描上方二维码
      </Text>
    </div>
  )
}

const qrFrameStyle = {
  width: 220,
  height: 220,
  margin: '0 auto',
  padding: 10,
  border: '1px solid #eee',
  borderRadius: 8,
  background: '#fff',
  position: 'relative',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
}

const overlayStyle = {
  position: 'absolute',
  inset: 0,
  display: 'flex',
  flexDirection: 'column',
  alignItems: 'center',
  justifyContent: 'center',
  background: 'rgba(255,255,255,0.92)',
  borderRadius: 8,
}
