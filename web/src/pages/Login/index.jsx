import React, { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { Button, Card, Form, TabPane, Tabs, Toast, Typography } from '@douyinfe/semi-ui'
import { storeLurusToken, isLoggedIn } from '../../auth'
import { useStore } from '../../store'

const { Title, Text } = Typography

export default function LoginPage() {
  const navigate = useNavigate()
  const init = useStore((s) => s.init)

  const [activeTab, setActiveTab] = useState('email')
  const [submitting, setSubmitting] = useState(false)

  // Already logged in — redirect immediately.
  if (isLoggedIn()) {
    const returnTo = sessionStorage.getItem('login_return') || '/hub'
    sessionStorage.removeItem('login_return')
    window.location.href = returnTo
    return null
  }

  async function handleEmailSubmit(values) {
    setSubmitting(true)
    try {
      const res = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          identifier: values.username,
          password: values.password,
        }),
      })
      const data = await res.json()
      if (!res.ok) {
        throw new Error(data.error || 'login failed')
      }
      storeLurusToken(data.token)
      await init()
      const returnTo = sessionStorage.getItem('login_return') || '/hub'
      sessionStorage.removeItem('login_return')
      navigate(returnTo, { replace: true })
    } catch (err) {
      Toast.error(err.message || 'login failed')
      setSubmitting(false)
    }
  }

  function handleWechatLogin() {
    sessionStorage.setItem('login_return', sessionStorage.getItem('login_return') || '/hub')
    window.location.href = '/api/v1/auth/wechat'
  }

  return (
    <div style={wrapperStyle}>
      <Card style={cardStyle} shadows="always">
        {/* Logo + title */}
        <div style={{ textAlign: 'center', marginBottom: 28 }}>
          <Title heading={3} style={{ color: '#1677ff', marginBottom: 2 }}>Lurus</Title>
          <Text type="secondary" size="small">登录你的账号</Text>
        </div>

        <Tabs activeKey={activeTab} onChange={setActiveTab} centered>
          {/* Account login tab */}
          <TabPane itemKey="email" tab="账号登录">
            <Form onSubmit={handleEmailSubmit} style={{ marginTop: 16 }}>
              <Form.Input
                field="username"
                label="账号"
                type="text"
                placeholder="用户名 / 手机号 / 邮箱"
                rules={[{ required: true, message: '请输入账号' }]}
              />
              <Form.Input
                field="password"
                label="密码"
                mode="password"
                placeholder="••••••••"
                rules={[{ required: true, message: '请输入密码' }]}
              />
              <div style={{ textAlign: 'right', marginBottom: 8 }}>
                <Link to="/forgot-password" style={{ fontSize: 12, color: '#1677ff' }}>
                  忘记密码?
                </Link>
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
              <Link to="/register" style={{ fontSize: 12, color: '#1677ff', marginLeft: 4 }}>
                去注册
              </Link>
            </div>
          </TabPane>

          {/* WeChat QR tab */}
          <TabPane itemKey="wechat" tab="微信扫码">
            <div style={{ textAlign: 'center', padding: '24px 0' }}>
              <div style={{ marginBottom: 16 }}>
                <div style={wechatIconWrapStyle}>
                  <WechatIcon size={32} />
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

function WechatIcon({ size = 18 }) {
  return (
    <svg
      width={size} height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      style={{ verticalAlign: 'middle', marginRight: 6 }}
    >
      <path d="M8.5 11a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zm7 0a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zM12 2C6.477 2 2 6.253 2 11.5c0 2.97 1.44 5.61 3.7 7.37L5 22l3.05-1.54A10.16 10.16 0 0 0 12 21c5.523 0 10-4.253 10-9.5S17.523 2 12 2z" />
    </svg>
  )
}

const wrapperStyle = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  minHeight: '100vh',
  background: '#f5f5f5',
}

const cardStyle = {
  width: 380,
  padding: '32px 28px',
}

const wechatIconWrapStyle = {
  width: 64, height: 64, borderRadius: '50%',
  background: '#f6ffed', border: '1px solid #b7eb8f',
  display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
  color: '#07c160',
}
