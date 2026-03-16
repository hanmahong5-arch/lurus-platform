import React, { useEffect, useState } from 'react'
import { Button, Card, Form, Input, TabPane, Tabs, Toast, Typography } from '@douyinfe/semi-ui'
import { getAuthInfo, submitPassword, linkWechatAndComplete } from '../../api/zlogin'

const { Title, Text } = Typography

// Session storage key used to remember the pending OIDC request during WeChat redirect.
const OIDC_REQ_KEY = 'zlogin_oidc_req'

/**
 * ZLogin — custom Zitadel OIDC login page.
 *
 * Zitadel redirects here when ZITADEL_LOGIN_URL is configured to point to identity.lurus.cn/zlogin.
 * Query params: authRequestId (required), plus Zitadel-appended params (id, nonce, etc.)
 */
export default function ZLogin() {
  const params      = new URLSearchParams(window.location.search)
  const authReqId   = params.get('authRequestId') || params.get('id') || ''

  const [activeTab,  setActiveTab]  = useState('email')
  const [appName,    setAppName]    = useState('Lurus')
  const [submitting, setSubmitting] = useState(false)

  // Fetch app info (best-effort — failure is non-fatal)
  useEffect(() => {
    if (!authReqId) return
    getAuthInfo(authReqId)
      .then((info) => {
        const name = info?.applicationName || info?.app_name || info?.clientId || ''
        if (name) setAppName(name)
      })
      .catch(() => {/* use default */})
  }, [authReqId])

  // After WeChat login returns lurus_token, attempt to bridge to OIDC
  useEffect(() => {
    const token   = localStorage.getItem('lurus_token')
    const pending = sessionStorage.getItem(OIDC_REQ_KEY)
    if (!token || !pending) return

    // Clear pending marker immediately to prevent loop
    sessionStorage.removeItem(OIDC_REQ_KEY)
    linkWechatAndComplete(pending, token)
      .then(({ callback_url }) => {
        window.location.href = callback_url
      })
      .catch((err) => {
        Toast.error('微信登录关联失败：' + err.message)
      })
  }, [])

  if (!authReqId) {
    return (
      <div style={wrapperStyle}>
        <Card style={cardStyle} shadows="always">
          <div style={{ textAlign: 'center', padding: '24px 0' }}>
            <div style={{ fontSize: 40, marginBottom: 12 }}>⚠️</div>
            <Text type="warning">缺少 authRequestId 参数。</Text>
            <div style={{ marginTop: 12 }}>
              <Text type="tertiary" size="small">
                此页面仅供 OIDC 授权流程使用，请从产品登录入口访问。
              </Text>
            </div>
          </div>
        </Card>
      </div>
    )
  }

  async function handleEmailSubmit(values) {
    setSubmitting(true)
    try {
      const { callback_url } = await submitPassword(authReqId, values.username, values.password)
      window.location.href = callback_url
    } catch (err) {
      Toast.error('登录失败：' + (err.message || '请检查邮箱和密码'))
      setSubmitting(false)
    }
  }

  function handleWechatLogin() {
    // Store the authRequestId so the Callback page can complete the OIDC flow
    sessionStorage.setItem(OIDC_REQ_KEY, authReqId)
    // Redirect into the existing WeChat OAuth flow
    window.location.href = '/api/v1/auth/wechat'
  }

  return (
    <div style={wrapperStyle}>
      <Card style={cardStyle} shadows="always">
        {/* Logo + title */}
        <div style={{ textAlign: 'center', marginBottom: 28 }}>
          <Title heading={3} style={{ color: '#1677ff', marginBottom: 2 }}>Lurus</Title>
          <Text type="secondary" size="small">登录后访问 {appName}</Text>
        </div>

        <Tabs activeKey={activeTab} onChange={setActiveTab} centered>
          {/* ── Email / password tab ── */}
          <TabPane itemKey="email" tab="邮箱登录">
            <Form
              onSubmit={handleEmailSubmit}
              style={{ marginTop: 16 }}
            >
              <Form.Input
                field="username"
                label="邮箱"
                type="email"
                placeholder="your@email.com"
                rules={[{ required: true, message: '请输入邮箱' }]}
              />
              <Form.Input
                field="password"
                label="密码"
                mode="password"
                placeholder="••••••••"
                rules={[{ required: true, message: '请输入密码' }]}
              />
              <div style={{ textAlign: 'right', marginBottom: 8 }}>
                <a
                  href={`https://auth.lurus.cn/ui/login/loginname?authRequestID=${authReqId}`}
                  target="_self"
                  style={{ fontSize: 12, color: '#1677ff' }}
                >
                  忘记密码?
                </a>
              </div>
              <Button
                htmlType="submit"
                type="primary"
                size="large"
                block
                loading={submitting}
              >
                登录
              </Button>
            </Form>

            <div style={{ textAlign: 'center', marginTop: 16 }}>
              <Text type="tertiary" size="small">还没有账号？</Text>
              <a
                href={`https://auth.lurus.cn/ui/login/loginname?authRequestID=${authReqId}`}
                target="_self"
                style={{ fontSize: 12, color: '#1677ff', marginLeft: 4 }}
              >
                去注册
              </a>
            </div>
          </TabPane>

          {/* ── WeChat QR tab ── */}
          <TabPane itemKey="wechat" tab="微信扫码">
            <div style={{ textAlign: 'center', padding: '24px 0' }}>
              <div style={{ marginBottom: 16 }}>
                <div
                  style={{
                    width: 64, height: 64, borderRadius: '50%',
                    background: '#f6ffed', border: '1px solid #b7eb8f',
                    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 32,
                  }}
                >
                  💬
                </div>
              </div>
              <Text style={{ display: 'block', marginBottom: 20 }} type="secondary">
                使用微信扫码快速登录
              </Text>
              <Button
                size="large"
                block
                onClick={handleWechatLogin}
                style={{ background: '#07c160', color: '#fff', border: 'none' }}
                icon={<WechatIcon />}
              >
                微信扫码登录
              </Button>
              <Text type="tertiary" size="small" style={{ display: 'block', marginTop: 12 }}>
                扫码后将自动跳转回 {appName}
              </Text>
            </div>
          </TabPane>
        </Tabs>

        {/* Footer */}
        <div style={{ textAlign: 'center', marginTop: 24, borderTop: '1px solid #f0f0f0', paddingTop: 16 }}>
          <Text type="tertiary" size="small">
            继续即代表同意{' '}
            <a href="https://lurus.cn/terms" target="_blank" rel="noopener noreferrer" style={{ color: '#1677ff' }}>
              服务条款
            </a>{' '}
            与{' '}
            <a href="https://lurus.cn/privacy" target="_blank" rel="noopener noreferrer" style={{ color: '#1677ff' }}>
              隐私政策
            </a>
          </Text>
        </div>
      </Card>
    </div>
  )
}

function WechatIcon() {
  return (
    <svg
      width="18" height="18"
      viewBox="0 0 24 24"
      fill="currentColor"
      style={{ verticalAlign: 'middle', marginRight: 6 }}
    >
      <path d="M8.5 11a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zm7 0a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zM12 2C6.477 2 2 6.253 2 11.5c0 2.97 1.44 5.61 3.7 7.37L5 22l3.05-1.54A10.16 10.16 0 0 0 12 21c5.523 0 10-4.253 10-9.5S17.523 2 12 2z" />
    </svg>
  )
}

const wrapperStyle = {
  display:        'flex',
  alignItems:     'center',
  justifyContent: 'center',
  minHeight:      '100vh',
  background:     '#f5f5f5',
}

const cardStyle = {
  width:   380,
  padding: '32px 28px',
}
