import React, { useEffect, useState } from 'react'
import {
  Card, Typography, Button, InputNumber, Modal, Toast, Spin, Divider
} from '@douyinfe/semi-ui'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { getTopupInfo, createTopup, getOrder } from '../../api/wallet'
import { useStore } from '../../store'

const { Title, Text } = Typography

const QUICK_AMOUNTS = [10, 30, 100, 300, 500]

// StaticQRSection shows admin-uploaded static QR codes for manual transfers.
function StaticQRSection() {
  const [alipayOk, setAlipayOk] = useState(false)
  const [wechatOk, setWechatOk] = useState(false)

  // We probe the endpoints; if they return 204 (empty), we hide the section.
  useEffect(() => {
    fetch('/api/v1/public/qrcode/alipay')
      .then(r => { if (r.ok && r.status === 200) setAlipayOk(true) })
      .catch(() => {})
    fetch('/api/v1/public/qrcode/wechat')
      .then(r => { if (r.ok && r.status === 200) setWechatOk(true) })
      .catch(() => {})
  }, [])

  if (!alipayOk && !wechatOk) return null

  return (
    <Card style={{ marginTop: 16 }}>
      <Divider>扫码收款（小额）</Divider>
      <div style={{ display: 'flex', gap: 32, justifyContent: 'center', padding: '8px 0' }}>
        {alipayOk && (
          <div style={{ textAlign: 'center' }}>
            <img
              src="/api/v1/public/qrcode/alipay"
              alt="支付宝收款码"
              style={{ width: 140, height: 140, objectFit: 'contain' }}
            />
            <div style={{ marginTop: 6, fontSize: 13, color: '#595959' }}>支付宝</div>
          </div>
        )}
        {wechatOk && (
          <div style={{ textAlign: 'center' }}>
            <img
              src="/api/v1/public/qrcode/wechat"
              alt="微信收款码"
              style={{ width: 140, height: 140, objectFit: 'contain' }}
            />
            <div style={{ marginTop: 6, fontSize: 13, color: '#595959' }}>微信</div>
          </div>
        )}
      </div>
      <div style={{ textAlign: 'center', fontSize: 12, color: '#8c8c8c', marginTop: 8 }}>
        转账后请截图发给客服，客服确认后手动入账
      </div>
    </Card>
  )
}

export default function TopupPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { refreshWallet } = useStore()

  const [methods, setMethods] = useState([])
  const [amount, setAmount] = useState(100)
  const [customAmount, setCustomAmount] = useState('')
  const [method, setMethod] = useState('')
  const [confirmVisible, setConfirmVisible] = useState(false)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    getTopupInfo().then((res) => {
      const list = res.data.payment_methods ?? []
      setMethods(list)
      if (list.length > 0) setMethod(list[0].id)
    })
  }, [])

  // Result polling (when returning from payment)
  const orderNo = searchParams.get('order_no')
  const [pollResult, setPollResult] = useState(null)
  const [polling, setPolling] = useState(false)
  const [countdown, setCountdown] = useState(300) // 5 minutes in seconds

  useEffect(() => {
    if (!orderNo) return
    setPolling(true)
    setCountdown(300)
    const start = Date.now()
    const MAX_MS = 5 * 60 * 1000

    // Countdown ticker
    const countdownTimer = setInterval(() => {
      const elapsed = Math.floor((Date.now() - start) / 1000)
      const remaining = Math.max(0, 300 - elapsed)
      setCountdown(remaining)
    }, 1000)

    // Order polling
    const pollTimer = setInterval(async () => {
      if (Date.now() - start > MAX_MS) {
        clearInterval(pollTimer)
        clearInterval(countdownTimer)
        setPolling(false)
        setPollResult('timeout')
        return
      }
      try {
        const res = await getOrder(orderNo)
        if (res.data.status === 'paid') {
          clearInterval(pollTimer)
          clearInterval(countdownTimer)
          setPolling(false)
          setPollResult('success')
          refreshWallet()
        } else if (res.data.status === 'failed' || res.data.status === 'cancelled') {
          clearInterval(pollTimer)
          clearInterval(countdownTimer)
          setPolling(false)
          setPollResult('failed')
        }
      } catch (_) {}
    }, 3000)

    return () => {
      clearInterval(pollTimer)
      clearInterval(countdownTimer)
    }
  }, [orderNo])

  const actualAmount = customAmount ? parseFloat(customAmount) : amount

  async function handleConfirm() {
    setLoading(true)
    try {
      const res = await createTopup({
        amount_cny: actualAmount,
        payment_method: method,
        return_url: window.location.origin + '/topup',
      })
      const { pay_url, order_no } = res.data
      if (pay_url) window.open(pay_url, '_blank')
      navigate(`/topup?order_no=${order_no}`)
    } catch (err) {
      Toast.error(err.response?.data?.error ?? '创建订单失败')
    } finally {
      setLoading(false)
      setConfirmVisible(false)
    }
  }

  if (polling) {
    const mins = Math.floor(countdown / 60)
    const secs = countdown % 60
    return (
      <div style={{ textAlign: 'center', marginTop: 80 }}>
        <Spin size="large" />
        <div style={{ marginTop: 16, fontSize: 16 }}>正在等待支付结果...</div>
        <div style={{ marginTop: 8, color: '#8c8c8c', fontSize: 14 }}>
          订单号：{orderNo}
        </div>
        <div style={{ marginTop: 8, color: countdown <= 30 ? '#f5222d' : '#8c8c8c', fontSize: 13 }}>
          {mins}:{String(secs).padStart(2, '0')} 后超时
        </div>
        <div style={{ marginTop: 16, color: '#8c8c8c', fontSize: 12 }}>
          已在新标签页打开支付页面，请完成支付后回到此页面
        </div>
      </div>
    )
  }

  if (pollResult === 'success') {
    return (
      <div style={{ textAlign: 'center', marginTop: 80 }}>
        <div style={{ fontSize: 48 }}>✅</div>
        <Title heading={4} style={{ marginTop: 16 }}>充值成功！</Title>
        <Button type="primary" onClick={() => navigate('/wallet')} style={{ marginTop: 16 }}>
          查看钱包
        </Button>
      </div>
    )
  }

  if (pollResult === 'timeout') {
    return (
      <div style={{ textAlign: 'center', marginTop: 80 }}>
        <div style={{ fontSize: 48 }}>⏳</div>
        <Title heading={4} style={{ marginTop: 16 }}>等待超时</Title>
        <div style={{ color: '#8c8c8c', marginTop: 8 }}>
          如果你已完成支付，余额将在几分钟内到账。<br />可以前往钱包查看，或重新发起充值。
        </div>
        <div style={{ display: 'flex', gap: 12, justifyContent: 'center', marginTop: 20 }}>
          <Button type="primary" onClick={() => navigate('/wallet')}>查看钱包</Button>
          <Button onClick={() => navigate('/topup')}>重新充值</Button>
        </div>
      </div>
    )
  }

  if (pollResult === 'failed') {
    return (
      <div style={{ textAlign: 'center', marginTop: 80 }}>
        <div style={{ fontSize: 48 }}>❌</div>
        <Title heading={4} style={{ marginTop: 16 }}>支付未完成</Title>
        <div style={{ color: '#8c8c8c', marginTop: 8 }}>订单已取消或支付失败</div>
        <Button onClick={() => navigate('/topup')} style={{ marginTop: 16 }}>重新充值</Button>
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 600 }}>
      <Title heading={3} style={{ marginBottom: 24 }}>钱包充值</Title>

      <Card title="选择金额" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, marginBottom: 16 }}>
          {QUICK_AMOUNTS.map((v) => (
            <Button
              key={v}
              type={amount === v && !customAmount ? 'primary' : 'tertiary'}
              onClick={() => { setAmount(v); setCustomAmount('') }}
            >
              ¥{v}
            </Button>
          ))}
        </div>
        <InputNumber
          placeholder="自定义金额"
          value={customAmount}
          onChange={(v) => setCustomAmount(v)}
          min={1}
          max={50000}
          prefix="¥"
          style={{ width: 200 }}
        />
      </Card>

      <Card title="支付方式" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
          {methods.map((m) => (
            <Button
              key={m.id}
              type={method === m.id ? 'primary' : 'tertiary'}
              onClick={() => setMethod(m.id)}
            >
              {m.name}
            </Button>
          ))}
        </div>
      </Card>

      <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
        <Button
          type="primary"
          size="large"
          disabled={!method || !actualAmount || actualAmount <= 0}
          onClick={() => setConfirmVisible(true)}
        >
          确认充值 ¥{actualAmount}
        </Button>
      </div>

      <StaticQRSection />

      <Modal
        title="确认充值"
        visible={confirmVisible}
        onOk={handleConfirm}
        onCancel={() => setConfirmVisible(false)}
        okText="确认支付"
        confirmLoading={loading}
      >
        <p>充值金额：<strong>¥{actualAmount}</strong></p>
        <p>支付方式：<strong>{methods.find(m => m.id === method)?.name}</strong></p>
        <p>点击确认后将打开支付页面，请在新标签页完成支付。</p>
      </Modal>
    </div>
  )
}
