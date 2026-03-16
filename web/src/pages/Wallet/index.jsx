import React, { useEffect, useState } from 'react'
import { Card, Typography, Button, Table, Tag, Select, Toast, Progress } from '@douyinfe/semi-ui'
import { useNavigate } from 'react-router-dom'
import { useStore } from '../../store'
import { listTransactions } from '../../api/wallet'

const { Title, Text } = Typography

const TX_TYPE_MAP = {
  topup:            { label: '充值',     color: 'green' },
  subscription:     { label: '订阅扣款', color: 'orange' },
  product_purchase: { label: '购买',     color: 'orange' },
  refund:           { label: '退款',     color: 'light-blue' },
  bonus:            { label: '奖励',     color: 'teal' },
  referral_reward:  { label: '推荐奖励', color: 'teal' },
  redemption:       { label: '兑换码',   color: 'purple' },
  checkin_reward:   { label: '签到',     color: 'cyan' },
  admin_credit:     { label: '管理员入账', color: 'green' },
  admin_debit:      { label: '管理员扣款', color: 'red' },
}

const columns = [
  {
    title: '类型',
    dataIndex: 'type',
    render: (t) => {
      const info = TX_TYPE_MAP[t] ?? { label: t, color: 'grey' }
      return <Tag color={info.color} size="small">{info.label}</Tag>
    },
  },
  {
    title: '金额',
    dataIndex: 'amount',
    render: (v) => (
      <span style={{ color: v >= 0 ? '#52c41a' : '#f5222d', fontWeight: 600 }}>
        {v >= 0 ? '+' : ''}{v.toFixed(4)} CNY
      </span>
    ),
  },
  { title: '余额', dataIndex: 'balance_after', render: (v) => `${v.toFixed(4)} CNY` },
  { title: '描述', dataIndex: 'description' },
  {
    title: '时间',
    dataIndex: 'created_at',
    render: (v) => new Date(v).toLocaleString('zh-CN'),
  },
]

const TYPE_OPTIONS = Object.entries(TX_TYPE_MAP).map(([value, { label }]) => ({
  label, value,
}))

// Holder tier thresholds and display info.
const HOLDER_TIERS = [
  { tier: 'diamond', label: '钻石持有者', minLB: 2000, color: '#722ed1', emoji: '💎', discount: '8.5折' },
  { tier: 'gold',    label: '黄金持有者', minLB: 500,  color: '#faad14', emoji: '🥇', discount: '9折'   },
  { tier: 'silver',  label: '白银持有者', minLB: 100,  color: '#8c8c8c', emoji: '🥈', discount: '9.5折' },
]

const NEXT_TIER = {
  silver:  { tier: 'gold',    label: '黄金', minLB: 500  },
  gold:    { tier: 'diamond', label: '钻石', minLB: 2000 },
  diamond: null,
}

// HolderTierCard displays the user's LB holder tier and the next upgrade progress.
function HolderTierCard({ wallet, onTopup }) {
  if (!wallet) return null

  const balance = wallet.balance ?? 0
  const current = HOLDER_TIERS.find((t) => balance >= t.minLB) ?? null
  const nextTier = current ? NEXT_TIER[current.tier] : { tier: 'silver', label: '白银', minLB: 100 }

  const tierLabel = current?.label ?? '普通用户'
  const tierColor = current?.color ?? '#1677ff'
  const tierEmoji = current?.emoji ?? '🔵'
  const discount  = current?.discount ?? '无折扣'

  return (
    <Card shadows="always" style={{ marginBottom: 24, borderLeft: `4px solid ${tierColor}` }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
            <span style={{ fontSize: 22 }}>{tierEmoji}</span>
            <span style={{ fontWeight: 700, fontSize: 16, color: tierColor }}>{tierLabel}</span>
          </div>
          <div style={{ display: 'flex', gap: 20, flexWrap: 'wrap' }}>
            <div>
              <Text type="secondary" size="small">当前余额</Text>
              <div style={{ fontWeight: 600 }}>{balance.toFixed(2)} LB</div>
            </div>
            <div>
              <Text type="secondary" size="small">购买折扣</Text>
              <div style={{ fontWeight: 600, color: tierColor }}>{discount}</div>
            </div>
          </div>
        </div>

        {nextTier && (
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text type="secondary" size="small" style={{ display: 'block', marginBottom: 6 }}>
              距 {nextTier.label} 还差 {Math.max(0, nextTier.minLB - balance).toFixed(2)} LB
            </Text>
            <Progress
              percent={Math.min(100, Math.round((balance / nextTier.minLB) * 100))}
              strokeColor={tierColor}
              size="small"
              showInfo={false}
            />
          </div>
        )}

        <Button type="primary" onClick={onTopup}>去充值升级</Button>
      </div>
    </Card>
  )
}

// UserCard shows the LurusID and linked login methods at the top of the wallet page.
function UserCard({ account }) {
  if (!account) return null

  const lurusID = account.lurus_id || ''

  function copyLurusID() {
    if (!lurusID) return
    navigator.clipboard.writeText(lurusID)
      .then(() => Toast.success('Lurus 号已复制'))
      .catch(() => Toast.error('复制失败，请手动复制'))
  }

  // Detect linked methods from email/zitadel_sub fields.
  const hasEmail = account.email && !account.email.startsWith('wechat.')
  const hasWechat = account.email?.startsWith('wechat.') ||
    (account.zitadel_sub && account.zitadel_sub.startsWith('wechat:'))

  return (
    <Card shadows="always" style={{ marginBottom: 24 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
        <div
          style={{
            width: 48, height: 48, borderRadius: '50%',
            background: '#1677ff', display: 'flex', alignItems: 'center', justifyContent: 'center',
            color: '#fff', fontSize: 20, fontWeight: 700, flexShrink: 0,
          }}
        >
          {(account.display_name || account.nickname || 'U')[0].toUpperCase()}
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontWeight: 600, fontSize: 16, marginBottom: 4 }}>
            {account.display_name || account.nickname || '用户'}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            <Text type="secondary" style={{ fontSize: 13 }}>
              Lurus 号：<span style={{ fontFamily: 'monospace', color: '#1677ff' }}>{lurusID}</span>
            </Text>
            {lurusID && (
              <Button size="small" type="tertiary" onClick={copyLurusID}>
                复制
              </Button>
            )}
          </div>
          <div style={{ display: 'flex', gap: 6, marginTop: 6 }}>
            {hasEmail && <Tag color="blue" size="small">邮箱 ✓</Tag>}
            {hasWechat && <Tag color="green" size="small">微信 ✓</Tag>}
          </div>
        </div>
      </div>
    </Card>
  )
}

export default function WalletPage() {
  const navigate = useNavigate()
  const { wallet, account, subscriptions, refreshWallet } = useStore()
  const [txList, setTxList] = useState([])
  const [txTotal, setTxTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [typeFilter, setTypeFilter] = useState(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    refreshWallet()
  }, [])

  useEffect(() => {
    setLoading(true)
    const params = { page, page_size: 20 }
    if (typeFilter) params.type = typeFilter
    listTransactions(params)
      .then((res) => {
        setTxList(res.data.data ?? [])
        setTxTotal(res.data.total ?? 0)
      })
      .finally(() => setLoading(false))
  }, [page, typeFilter])

  function handleFilterChange(val) {
    setTypeFilter(val ?? null)
    setPage(1)
  }

  return (
    <div>
      <Title heading={3} style={{ marginBottom: 24 }}>我的钱包</Title>
      <UserCard account={account} />
      <HolderTierCard wallet={wallet} onTopup={() => navigate('/topup')} />

      <div style={{ display: 'flex', gap: 16, marginBottom: 24 }}>
        <Card style={{ flex: 1 }} shadows="always">
          <Text type="secondary">可用余额</Text>
          <div style={{ fontSize: 32, fontWeight: 700, color: '#1677ff', margin: '8px 0' }}>
            ¥ {wallet?.balance?.toFixed(2) ?? '0.00'}
          </div>
          <Button type="primary" onClick={() => navigate('/topup')}>立即充值</Button>
        </Card>
        <Card style={{ flex: 1 }} shadows="always">
          <Text type="secondary">历史累计充值</Text>
          <div style={{ fontSize: 24, fontWeight: 600, marginTop: 8 }}>
            ¥ {wallet?.lifetime_topup?.toFixed(2) ?? '0.00'}
          </div>
        </Card>
        <Card style={{ flex: 1 }} shadows="always">
          <Text type="secondary">活跃订阅</Text>
          <div style={{ fontSize: 24, fontWeight: 600, marginTop: 8 }}>
            {subscriptions.filter(s => s.status === 'active').length} 个
          </div>
        </Card>
      </div>

      <Card
        title="交易流水"
        headerExtraContent={
          <Select
            placeholder="筛选类型"
            optionList={TYPE_OPTIONS}
            value={typeFilter}
            onChange={handleFilterChange}
            showClear
            style={{ width: 140 }}
            size="small"
          />
        }
      >
        <Table
          columns={columns}
          dataSource={txList}
          loading={loading}
          rowKey="id"
          empty={
            <div style={{ textAlign: 'center', padding: '40px 0', color: '#8c8c8c' }}>
              <div style={{ fontSize: 32, marginBottom: 8 }}>📭</div>
              <div>{typeFilter ? `暂无「${TX_TYPE_MAP[typeFilter]?.label ?? typeFilter}」类型的交易记录` : '暂无交易记录'}</div>
            </div>
          }
          pagination={{
            total: txTotal,
            currentPage: page,
            pageSize: 20,
            onPageChange: setPage,
          }}
        />
      </Card>
    </div>
  )
}
