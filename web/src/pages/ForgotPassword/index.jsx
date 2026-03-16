import React, { useState } from 'react'
import { Link } from 'react-router-dom'
import { Button, Card, Form, Toast, Typography } from '@douyinfe/semi-ui'

const { Title, Text } = Typography

export default function ForgotPasswordPage() {
  const [step, setStep] = useState(1)
  const [submitting, setSubmitting] = useState(false)
  const [identifier, setIdentifier] = useState('')
  const [channel, setChannel] = useState('')

  async function handleStep1(values) {
    setSubmitting(true)
    try {
      const res = await fetch('/api/v1/auth/forgot-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ identifier: values.identifier }),
      })
      const data = await res.json()
      if (!res.ok) {
        throw new Error(data.error || 'request failed')
      }
      setIdentifier(values.identifier)
      setChannel(data.channel || 'email')
      setStep(2)
    } catch (err) {
      Toast.error(err.message || 'request failed')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleStep2(values) {
    setSubmitting(true)
    try {
      const res = await fetch('/api/v1/auth/reset-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          identifier,
          code: values.code,
          new_password: values.new_password,
        }),
      })
      const data = await res.json()
      if (!res.ok) {
        throw new Error(data.error || 'reset failed')
      }
      Toast.success('密码重置成功，请重新登录')
      window.location.href = '/login'
    } catch (err) {
      Toast.error(err.message || 'reset failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div style={wrapperStyle}>
      <Card style={cardStyle} shadows="always">
        <div style={{ textAlign: 'center', marginBottom: 28 }}>
          <Title heading={3} style={{ color: '#1677ff', marginBottom: 2 }}>Lurus</Title>
          <Text type="secondary" size="small">找回密码</Text>
        </div>

        {step === 1 && (
          <Form onSubmit={handleStep1}>
            <Form.Input
              field="identifier"
              label="账号"
              placeholder="用户名 / 手机号 / 邮箱"
              rules={[{ required: true, message: '请输入账号' }]}
            />
            <Button
              htmlType="submit"
              type="primary"
              size="large"
              block
              loading={submitting}
            >
              发送验证码
            </Button>
          </Form>
        )}

        {step === 2 && (
          <>
            <div style={{ marginBottom: 16, textAlign: 'center' }}>
              <Text type="secondary">
                {channel === 'sms'
                  ? '验证码已发送到您的手机'
                  : '验证码已发送到您的邮箱'}
              </Text>
            </div>
            <Form onSubmit={handleStep2}>
              <Form.Input
                field="code"
                label="验证码"
                placeholder="请输入验证码"
                rules={[{ required: true, message: '请输入验证码' }]}
              />
              <Form.Input
                field="new_password"
                label="新密码"
                mode="password"
                placeholder="至少8位"
                rules={[
                  { required: true, message: '请输入新密码' },
                  { validator: (_, v) => !v || v.length >= 8, message: '密码至少8位' },
                ]}
              />
              <Button
                htmlType="submit"
                type="primary"
                size="large"
                block
                loading={submitting}
              >
                重置密码
              </Button>
            </Form>
          </>
        )}

        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Link to="/login" style={{ fontSize: 12, color: '#1677ff' }}>
            返回登录
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
