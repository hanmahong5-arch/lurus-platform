import React, { useState } from 'react'
import { Card, Typography, Input, Button, Toast, Banner } from '@douyinfe/semi-ui'
import { redeem } from '../../api/wallet'
import { useStore } from '../../store'

const { Title, Text } = Typography

export default function RedeemPage() {
  const [code, setCode] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState(null) // 'success' | 'error' | null
  const [errMsg, setErrMsg] = useState('')
  const { refreshWallet } = useStore()

  async function handleRedeem() {
    const trimmed = code.trim().toUpperCase()
    if (!trimmed) return
    setLoading(true)
    setResult(null)
    try {
      await redeem(trimmed)
      setResult('success')
      setCode('')
      refreshWallet()
    } catch (err) {
      setResult('error')
      setErrMsg(err.response?.data?.error ?? '兑换失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ maxWidth: 480 }}>
      <Title heading={3} style={{ marginBottom: 24 }}>兑换码</Title>

      <Card>
        <Text type="secondary" style={{ marginBottom: 16, display: 'block' }}>
          输入兑换码，即可获得对应余额或订阅权益。
        </Text>

        <Input
          size="large"
          value={code}
          onChange={setCode}
          placeholder="请输入兑换码（不区分大小写）"
          style={{ marginBottom: 16, letterSpacing: 2 }}
          onEnterPress={handleRedeem}
        />

        <Button
          type="primary"
          size="large"
          loading={loading}
          disabled={!code.trim()}
          onClick={handleRedeem}
          block
        >
          立即兑换
        </Button>

        {result === 'success' && (
          <Banner
            type="success"
            description="兑换成功！余额已更新。"
            style={{ marginTop: 16 }}
          />
        )}
        {result === 'error' && (
          <Banner
            type="danger"
            description={errMsg}
            style={{ marginTop: 16 }}
          />
        )}
      </Card>
    </div>
  )
}
