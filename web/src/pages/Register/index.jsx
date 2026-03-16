import React, { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { Button, Card, Form, Toast, Typography } from '@douyinfe/semi-ui'
import { storeLurusToken, isLoggedIn } from '../../auth'
import { useStore } from '../../store'

const { Title, Text } = Typography

export default function RegisterPage() {
  const navigate = useNavigate()
  const init = useStore((s) => s.init)
  const [submitting, setSubmitting] = useState(false)

  if (isLoggedIn()) {
    const returnTo = sessionStorage.getItem('login_return') || '/hub'
    sessionStorage.removeItem('login_return')
    window.location.href = returnTo
    return null
  }

  async function handleSubmit(values) {
    setSubmitting(true)
    try {
      const res = await fetch('/api/v1/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: values.username,
          password: values.password,
          email: values.email || undefined,
          phone: values.phone || undefined,
          aff_code: values.aff_code || undefined,
        }),
      })
      const data = await res.json()
      if (!res.ok) {
        throw new Error(data.error || 'registration failed')
      }
      storeLurusToken(data.token)
      await init()
      navigate('/hub', { replace: true })
    } catch (err) {
      Toast.error(err.message || 'registration failed')
      setSubmitting(false)
    }
  }

  return (
    <div style={wrapperStyle}>
      <Card style={cardStyle} shadows="always">
        <div style={{ textAlign: 'center', marginBottom: 28 }}>
          <Title heading={3} style={{ color: '#1677ff', marginBottom: 2 }}>Lurus</Title>
          <Text type="secondary" size="small">创建新账号</Text>
        </div>

        <Form onSubmit={handleSubmit}>
          <Form.Input
            field="username"
            label="用户名"
            placeholder="3-32位字母数字下划线，或手机号"
            rules={[{ required: true, message: '请输入用户名' }]}
          />
          <Form.Input
            field="password"
            label="密码"
            mode="password"
            placeholder="至少8位"
            rules={[
              { required: true, message: '请输入密码' },
              { validator: (_, v) => !v || v.length >= 8, message: '密码至少8位' },
            ]}
          />
          <Form.Input
            field="email"
            label="邮箱（选填）"
            type="email"
            placeholder="用于找回密码"
          />
          <Form.Input
            field="phone"
            label="手机号（选填）"
            placeholder="11位中国手机号"
          />
          <Form.Input
            field="aff_code"
            label="邀请码（选填）"
            placeholder="如有邀请码请填写"
          />
          <Button
            htmlType="submit"
            type="primary"
            size="large"
            block
            loading={submitting}
            style={{ marginTop: 8 }}
          >
            注册
          </Button>
        </Form>

        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Text type="tertiary" size="small">已有账号？</Text>
          <Link to="/login" style={{ fontSize: 12, color: '#1677ff', marginLeft: 4 }}>
            去登录
          </Link>
        </div>
      </Card>
    </div>
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
  width: 400,
  padding: '32px 28px',
}
