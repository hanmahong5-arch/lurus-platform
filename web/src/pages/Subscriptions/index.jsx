import React, { useEffect, useState } from 'react'
import {
  Card, Typography, Button, Modal, Toast, Tag, Descriptions, Banner
} from '@douyinfe/semi-ui'
import { useStore } from '../../store'
import { listProducts, listPlans, checkout, cancelSubscription } from '../../api/subscription'
import { getTopupInfo } from '../../api/wallet'
import LurusBadge from '../../components/LurusBadge'

const { Title, Text } = Typography

const PLAN_CODE_LABEL = {
  free:       '免费版',
  basic:      'Basic',
  pro:        'Pro',
  enterprise: 'Enterprise',
}

const STATUS_TAG = {
  active:  { label: '有效', color: 'green' },
  expired: { label: '已到期', color: 'red' },
  grace:   { label: '宽限期', color: 'orange' },
  trial:   { label: '试用', color: 'cyan' },
  free:    { label: '免费', color: 'grey' },
}

function formatDate(d) {
  if (!d) return '永久'
  return new Date(d).toLocaleDateString('zh-CN')
}

function SubCard({ product, sub, onUpgrade, onCancel }) {
  const planCode = sub?.plan_code ?? 'free'
  const status = sub?.status ?? 'free'
  const expiresAt = sub?.expires_at
  const autoRenew = sub?.auto_renew
  const statusInfo = STATUS_TAG[status] ?? { label: status, color: 'grey' }
  const canCancel = sub && planCode !== 'free' && status === 'active'

  return (
    <Card
      shadows="hover"
      style={{ minWidth: 240, flex: '1 1 240px' }}
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <LurusBadge productId={product.id} planCode={planCode} />
          <span>{product.name}</span>
          <Tag color={statusInfo.color} size="small">{statusInfo.label}</Tag>
        </div>
      }
      footer={
        <div style={{ display: 'flex', gap: 8 }}>
          <Button type="primary" size="small" onClick={() => onUpgrade(product)}>
            {planCode === 'free' ? '立即订阅' : '升级套餐'}
          </Button>
          {canCancel && (
            <Button type="danger" size="small" theme="borderless" onClick={() => onCancel(product, sub)}>
              取消订阅
            </Button>
          )}
        </div>
      }
    >
      <Descriptions
        size="small"
        data={[
          { key: '当前套餐', value: PLAN_CODE_LABEL[planCode] ?? planCode },
          { key: '到期日期', value: formatDate(expiresAt) },
          { key: '自动续费', value: autoRenew ? '开启' : '未开启' },
        ]}
      />
    </Card>
  )
}

export default function SubscriptionsPage() {
  const { subscriptions, wallet, refreshSubscriptions, refreshWallet } = useStore()
  const [products, setProducts] = useState([])
  const [upgradeProduct, setUpgradeProduct] = useState(null)
  const [plans, setPlans] = useState([])
  const [selectedPlan, setSelectedPlan] = useState(null)
  const [methods, setMethods] = useState([])
  const [method, setMethod] = useState('wallet')
  const [loading, setLoading] = useState(false)
  const [cancelTarget, setCancelTarget] = useState(null) // { product, sub }
  const [cancelLoading, setCancelLoading] = useState(false)

  useEffect(() => {
    listProducts().then((res) => setProducts(res.data ?? []))
    getTopupInfo().then((res) => setMethods(res.data.payment_methods ?? []))
  }, [])

  async function openUpgrade(product) {
    setUpgradeProduct(product)
    const res = await listPlans(product.id)
    const pList = (res.data ?? []).filter((p) => p.status === 1 && p.code !== 'free')
    setPlans(pList)
    setSelectedPlan(pList[0] ?? null)
  }

  async function handleCheckout() {
    if (!selectedPlan) return
    setLoading(true)
    try {
      const res = await checkout({
        product_id: upgradeProduct.id,
        plan_id: selectedPlan.id,
        payment_method: method,
        return_url: window.location.origin + '/subscriptions',
      })
      if (res.data.pay_url) {
        window.open(res.data.pay_url, '_blank')
      }
      Toast.success('订阅成功')
      setUpgradeProduct(null)
      await Promise.all([refreshSubscriptions(), refreshWallet()])
    } catch (err) {
      Toast.error(err.response?.data?.error ?? '操作失败')
    } finally {
      setLoading(false)
    }
  }

  async function handleCancel() {
    if (!cancelTarget) return
    setCancelLoading(true)
    try {
      await cancelSubscription(cancelTarget.product.id)
      Toast.success('订阅已取消，到期前仍可正常使用')
      setCancelTarget(null)
      await refreshSubscriptions()
    } catch (err) {
      Toast.error(err.response?.data?.error ?? '取消失败')
    } finally {
      setCancelLoading(false)
    }
  }

  // Wallet balance warning for selected plan when using wallet payment
  const walletInsufficient =
    method === 'wallet' &&
    selectedPlan &&
    (wallet?.balance ?? 0) < selectedPlan.price_cny

  const subMap = Object.fromEntries(subscriptions.map((s) => [s.product_id, s]))

  return (
    <div>
      <Title heading={3} style={{ marginBottom: 24 }}>我的订阅</Title>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 16 }}>
        {products.map((p) => (
          <SubCard
            key={p.id}
            product={p}
            sub={subMap[p.id]}
            onUpgrade={openUpgrade}
            onCancel={(product, sub) => setCancelTarget({ product, sub })}
          />
        ))}
      </div>

      {/* Checkout modal */}
      <Modal
        title={`订阅 ${upgradeProduct?.name}`}
        visible={!!upgradeProduct}
        onOk={handleCheckout}
        onCancel={() => setUpgradeProduct(null)}
        okText="确认购买"
        okButtonProps={{ disabled: walletInsufficient }}
        confirmLoading={loading}
        width={480}
      >
        <div style={{ marginBottom: 16 }}>
          <Text type="secondary">选择套餐</Text>
          <div style={{ display: 'flex', gap: 10, marginTop: 8, flexWrap: 'wrap' }}>
            {plans.map((p) => (
              <Card
                key={p.id}
                style={{
                  cursor: 'pointer',
                  flex: 1,
                  border: selectedPlan?.id === p.id ? '2px solid #1677ff' : '1px solid #e5e5e5',
                }}
                onClick={() => setSelectedPlan(p)}
                bodyStyle={{ padding: '12px 16px' }}
              >
                <div style={{ fontWeight: 600 }}>{PLAN_CODE_LABEL[p.code] ?? p.code}</div>
                <div style={{ fontSize: 20, color: '#1677ff', margin: '4px 0' }}>
                  ¥{p.price_cny}
                </div>
                <div style={{ fontSize: 12, color: '#8c8c8c' }}>{p.billing_cycle}</div>
              </Card>
            ))}
          </div>
        </div>

        <div style={{ marginBottom: 12 }}>
          <Text type="secondary">支付方式</Text>
          <div style={{ display: 'flex', gap: 10, marginTop: 8, flexWrap: 'wrap' }}>
            <Button
              type={method === 'wallet' ? 'primary' : 'tertiary'}
              onClick={() => setMethod('wallet')}
            >
              钱包余额 (¥{wallet?.balance?.toFixed(2) ?? '0.00'})
            </Button>
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
        </div>

        {walletInsufficient && (
          <Banner
            type="warning"
            description={`钱包余额不足（¥${wallet?.balance?.toFixed(2) ?? '0.00'}），需 ¥${selectedPlan.price_cny}。请先充值或选择其他支付方式。`}
            closeIcon={null}
          />
        )}
      </Modal>

      {/* Cancel confirmation modal */}
      <Modal
        title="取消订阅"
        visible={!!cancelTarget}
        onOk={handleCancel}
        onCancel={() => setCancelTarget(null)}
        okText="确认取消"
        okButtonProps={{ type: 'danger' }}
        confirmLoading={cancelLoading}
        width={400}
      >
        <p>确认取消 <strong>{cancelTarget?.product.name}</strong> 的订阅？</p>
        <p style={{ color: '#8c8c8c', fontSize: 13 }}>
          取消后当前订阅周期结束前仍可正常使用，到期后不再续费。
        </p>
      </Modal>
    </div>
  )
}
