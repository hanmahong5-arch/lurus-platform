import React, { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Spin, Typography } from '@douyinfe/semi-ui'
import { handleCallback, storeLurusToken } from '../../auth'
import { linkWechatAndComplete } from '../../api/zlogin'
import { useStore } from '../../store'

// Session storage key set by ZLogin when the user triggers WeChat auth within an OIDC flow.
const OIDC_REQ_KEY = 'zlogin_oidc_req'

const { Text } = Typography

export default function CallbackPage() {
  const navigate = useNavigate()
  const init = useStore((s) => s.init)
  const [error, setError] = useState(null)

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)

    // WeChat login path: server redirected here with a lurus session token.
    const lurusToken = params.get('lurus_token')
    if (lurusToken) {
      storeLurusToken(lurusToken)

      // Check if this WeChat login was triggered from a Zitadel OIDC flow (/zlogin).
      const pendingOIDC = sessionStorage.getItem(OIDC_REQ_KEY)
      if (pendingOIDC) {
        sessionStorage.removeItem(OIDC_REQ_KEY)
        linkWechatAndComplete(pendingOIDC, lurusToken)
          .then(({ callback_url }) => {
            window.location.href = callback_url
          })
          .catch((err) => {
            setError('微信 OIDC 关联失败：' + err.message)
          })
        return
      }

      init().finally(() => {
        const returnTo = sessionStorage.getItem('login_return') || '/wallet'
        sessionStorage.removeItem('login_return')
        navigate(returnTo, { replace: true })
      })
      return
    }

    // Zitadel OIDC PKCE path.
    const code    = params.get('code')
    const err     = params.get('error')
    const errDesc = params.get('error_description')

    if (err) {
      setError(`${err}: ${errDesc || ''}`)
      return
    }

    if (!code) {
      setError('No authorization code received.')
      return
    }

    handleCallback(code)
      .then(() => {
        // Pre-load account data before navigating so the target page
        // doesn't start with empty state.
        return init()
      })
      .then(() => {
        const returnTo = sessionStorage.getItem('login_return') || '/wallet'
        sessionStorage.removeItem('login_return')
        navigate(returnTo, { replace: true })
      })
      .catch(e => setError(e.message))
  }, [])

  if (error) {
    return (
      <div style={{ textAlign: 'center', marginTop: 80 }}>
        <div style={{ fontSize: 40 }}>❌</div>
        <div style={{ marginTop: 16, color: '#f5222d' }}>登录失败</div>
        <Text type="tertiary" style={{ fontSize: 13 }}>{error}</Text>
        <div style={{ marginTop: 16 }}>
          <a href="/login">重新登录</a>
        </div>
      </div>
    )
  }

  return (
    <div style={{ textAlign: 'center', marginTop: 80 }}>
      <Spin size="large" />
      <div style={{ marginTop: 16, color: '#8c8c8c' }}>正在完成登录...</div>
    </div>
  )
}
