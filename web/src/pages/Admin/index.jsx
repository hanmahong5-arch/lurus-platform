import React, { useState, useEffect, useRef, lazy, Suspense } from 'react'
import {
  Card, Typography, Table, Input, Button, Modal, InputNumber,
  Toast, Tag, Tabs, TabPane, Form, Toast as SemiToast, Spin,
} from '@douyinfe/semi-ui'
import axios from 'axios'

// Lazy-loaded NewAPI management tabs (only loaded when user clicks the tab).
const ChannelsTab = lazy(() => import('./ChannelsTab'))
const TokensTab = lazy(() => import('./TokensTab'))
const UsageLogsTab = lazy(() => import('./UsageLogsTab'))
const ModelsTab = lazy(() => import('./ModelsTab'))
const GatewaySettingsTab = lazy(() => import('./GatewaySettingsTab'))

function LazyTab({ children }) {
  return (
    <Suspense fallback={<div style={{ padding: 40, textAlign: 'center' }}><Spin size="large" /></div>}>
      {children}
    </Suspense>
  )
}

const { Title, Text } = Typography

const adminClient = axios.create({ baseURL: '/admin/v1', timeout: 15000 })
adminClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('lurus_token') || localStorage.getItem('token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

// ── Account List Tab ───────────────────────────────────────────────────────────

function AccountListTab() {
  const [accounts, setAccounts] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [adjustTarget, setAdjustTarget] = useState(null)
  const [adjustAmount, setAdjustAmount] = useState(0)
  const [adjustDesc, setAdjustDesc] = useState('')
  const [adjustLoading, setAdjustLoading] = useState(false)

  async function fetchAccounts(p = page, kw = keyword) {
    setLoading(true)
    try {
      const res = await adminClient.get('/accounts', { params: { page: p, page_size: 20, keyword: kw } })
      setAccounts(res.data.accounts ?? res.data.data ?? [])
      setTotal(res.data.total ?? 0)
    } catch {
      Toast.error('加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchAccounts() }, [page])

  async function handleAdjust() {
    if (!adjustTarget || !adjustDesc) return
    setAdjustLoading(true)
    try {
      await adminClient.post(`/accounts/${adjustTarget.id}/wallet/adjust`, {
        amount: adjustAmount,
        description: adjustDesc,
      })
      Toast.success('余额调整成功')
      setAdjustTarget(null)
      setAdjustAmount(0)
      setAdjustDesc('')
      fetchAccounts()
    } catch (err) {
      Toast.error(err.response?.data?.error ?? '操作失败')
    } finally {
      setAdjustLoading(false)
    }
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    { title: 'Lurus ID', dataIndex: 'lurus_id' },
    { title: '昵称', dataIndex: 'nickname' },
    { title: '邮箱', dataIndex: 'email' },
    {
      title: 'VIP',
      dataIndex: 'vip_level',
      render: (v) => v > 0 ? <Tag color="yellow">Lv.{v}</Tag> : <Tag>普通</Tag>,
    },
    {
      title: '操作',
      render: (_, row) => (
        <Button size="small" onClick={() => setAdjustTarget(row)}>余额调整</Button>
      ),
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', gap: 10, marginBottom: 16 }}>
        <Input
          value={keyword}
          onChange={setKeyword}
          placeholder="搜索邮箱 / Lurus ID"
          style={{ width: 280 }}
          onEnterPress={() => { setPage(1); fetchAccounts(1, keyword) }}
        />
        <Button onClick={() => { setPage(1); fetchAccounts(1, keyword) }}>搜索</Button>
      </div>

      <Card>
        <Table
          columns={columns}
          dataSource={accounts}
          loading={loading}
          rowKey="id"
          pagination={{
            total,
            currentPage: page,
            pageSize: 20,
            onPageChange: (p) => setPage(p),
          }}
        />
      </Card>

      <Modal
        title={`余额调整 — ${adjustTarget?.lurus_id ?? ''}`}
        visible={!!adjustTarget}
        onOk={handleAdjust}
        onCancel={() => setAdjustTarget(null)}
        okText="确认"
        confirmLoading={adjustLoading}
      >
        <div style={{ marginBottom: 12 }}>
          <div>金额（正数=入账，负数=扣款）</div>
          <InputNumber
            value={adjustAmount}
            onChange={setAdjustAmount}
            style={{ width: '100%', marginTop: 4 }}
          />
        </div>
        <div>
          <div>备注说明</div>
          <Input
            value={adjustDesc}
            onChange={setAdjustDesc}
            style={{ marginTop: 4 }}
            placeholder="请填写调整原因"
          />
        </div>
      </Modal>
    </div>
  )
}

// ── System Config Tab ──────────────────────────────────────────────────────────

const SECRET_KEYS = new Set([
  'epay_key', 'stripe_secret_key', 'stripe_webhook_secret',
  'creem_api_key', 'creem_webhook_secret',
])

const QR_KEYS = new Set(['qr_static_alipay', 'qr_static_wechat', 'qr_channel_promo'])

const QR_LABELS = {
  qr_static_alipay: '支付宝静态收款码',
  qr_static_wechat: '微信静态收款码',
  qr_channel_promo: '渠道推广码',
}

const QR_TYPES = {
  qr_static_alipay: 'alipay',
  qr_static_wechat: 'wechat',
  qr_channel_promo: 'channel',
}

const PROVIDER_SECTIONS = [
  {
    label: 'Epay 易支付',
    keys: ['epay_partner_id', 'epay_key', 'epay_gateway_url', 'epay_notify_url'],
    labels: {
      epay_partner_id:  '商户 ID',
      epay_key:         '签名密钥',
      epay_gateway_url: '网关 URL',
      epay_notify_url:  '回调 URL',
    },
  },
  {
    label: 'Stripe',
    keys: ['stripe_secret_key', 'stripe_webhook_secret'],
    labels: {
      stripe_secret_key:     'Secret Key',
      stripe_webhook_secret: 'Webhook 密钥',
    },
  },
  {
    label: 'Creem',
    keys: ['creem_api_key', 'creem_webhook_secret'],
    labels: {
      creem_api_key:         'API Key',
      creem_webhook_secret:  'Webhook 密钥',
    },
  },
]

function SystemConfigTab() {
  const [settings, setSettings] = useState({})  // key → { value, is_secret }
  const [edits, setEdits] = useState({})         // key → new value (string)
  const [revealed, setRevealed] = useState({})   // key → bool
  const [saving, setSaving] = useState(false)
  const [qrPreviews, setQrPreviews] = useState({}) // key → data URL
  const [qrUploading, setQrUploading] = useState({})
  const fileRefs = { qr_static_alipay: useRef(), qr_static_wechat: useRef(), qr_channel_promo: useRef() }

  async function fetchSettings() {
    try {
      const res = await adminClient.get('/settings')
      const map = {}
      for (const s of res.data.settings ?? []) {
        map[s.key] = s
      }
      setSettings(map)
    } catch {
      Toast.error('加载配置失败')
    }
  }

  useEffect(() => { fetchSettings() }, [])

  function getValue(key) {
    if (edits[key] !== undefined) return edits[key]
    const s = settings[key]
    if (!s) return ''
    // Secret placeholder from server → show empty to allow editing
    if (s.is_secret && s.value === '••••••••') return ''
    return s.value ?? ''
  }

  async function handleSave() {
    const paymentKeys = PROVIDER_SECTIONS.flatMap(s => s.keys)
    const items = paymentKeys
      .filter(k => edits[k] !== undefined && edits[k] !== '')
      .map(k => ({ key: k, value: edits[k] }))

    if (items.length === 0) {
      Toast.info('没有需要保存的更改')
      return
    }
    setSaving(true)
    try {
      await adminClient.put('/settings', { settings: items })
      Toast.success(`已保存 ${items.length} 项配置`)
      setEdits({})
      fetchSettings()
    } catch (err) {
      Toast.error(err.response?.data?.error ?? '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function handleQRUpload(key, file) {
    setQrUploading(prev => ({ ...prev, [key]: true }))
    try {
      const base64 = await fileToBase64(file)
      const qrType = QR_TYPES[key]
      await adminClient.post('/settings/qrcode', { type: qrType, image_base64: base64 })
      setQrPreviews(prev => ({ ...prev, [key]: `data:image/png;base64,${base64}` }))
      Toast.success(`${QR_LABELS[key]} 上传成功`)
    } catch (err) {
      Toast.error(err.response?.data?.error ?? '上传失败')
    } finally {
      setQrUploading(prev => ({ ...prev, [key]: false }))
    }
  }

  return (
    <div style={{ maxWidth: 700 }}>
      {PROVIDER_SECTIONS.map((section) => (
        <Card
          key={section.label}
          title={section.label}
          style={{ marginBottom: 16 }}
        >
          {section.keys.map((key) => {
            const isSecret = SECRET_KEYS.has(key)
            const isRevealed = revealed[key]
            const inputType = isSecret && !isRevealed ? 'password' : 'text'
            return (
              <div key={key} style={{ display: 'flex', alignItems: 'center', marginBottom: 12, gap: 8 }}>
                <Text style={{ width: 160, flexShrink: 0, color: '#595959' }}>
                  {section.labels[key]}
                </Text>
                <Input
                  type={inputType}
                  value={getValue(key)}
                  onChange={(v) => setEdits(prev => ({ ...prev, [key]: v }))}
                  placeholder={isSecret ? '输入后保存生效（留空=保持不变）' : ''}
                  style={{ flex: 1 }}
                />
                {isSecret && (
                  <Button
                    size="small"
                    type="tertiary"
                    onClick={() => setRevealed(prev => ({ ...prev, [key]: !prev[key] }))}
                  >
                    {isRevealed ? '隐藏' : '显示'}
                  </Button>
                )}
              </div>
            )
          })}
        </Card>
      ))}

      <Button type="primary" loading={saving} onClick={handleSave} style={{ marginBottom: 24 }}>
        保存支付配置
      </Button>

      <Card title="二维码管理" style={{ marginBottom: 16 }}>
        {Object.entries(QR_LABELS).map(([key, label]) => (
          <div key={key} style={{ display: 'flex', alignItems: 'center', marginBottom: 16, gap: 12 }}>
            <Text style={{ width: 160, flexShrink: 0, color: '#595959' }}>{label}</Text>
            <input
              ref={fileRefs[key]}
              type="file"
              accept="image/*"
              style={{ display: 'none' }}
              onChange={(e) => {
                const file = e.target.files?.[0]
                if (file) handleQRUpload(key, file)
                e.target.value = ''
              }}
            />
            <Button
              loading={!!qrUploading[key]}
              onClick={() => fileRefs[key].current?.click()}
              size="small"
            >
              选择图片
            </Button>
            {qrPreviews[key] && (
              <img
                src={qrPreviews[key]}
                alt={label}
                style={{ width: 80, height: 80, objectFit: 'contain', border: '1px solid #e8e8e8', borderRadius: 4 }}
              />
            )}
            {!qrPreviews[key] && settings[key]?.value && (
              <img
                src={`/api/v1/public/qrcode/${QR_TYPES[key]}`}
                alt={label}
                style={{ width: 80, height: 80, objectFit: 'contain', border: '1px solid #e8e8e8', borderRadius: 4 }}
                onError={(e) => { e.target.style.display = 'none' }}
              />
            )}
          </div>
        ))}
      </Card>
    </div>
  )
}

// Read a File as raw base64 (without data-URL prefix).
function fileToBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = (e) => {
      const dataUrl = e.target.result
      // Strip the "data:<mime>;base64," prefix.
      const base64 = dataUrl.split(',')[1]
      resolve(base64)
    }
    reader.onerror = reject
    reader.readAsDataURL(file)
  })
}

// ── Page root ─────────────────────────────────────────────────────────────────

export default function AdminPage() {
  return (
    <div>
      <Title heading={3} style={{ marginBottom: 24 }}>管理后台</Title>
      <Tabs type="line" lazyRender>
        <TabPane tab="账号列表" itemKey="accounts">
          <div style={{ paddingTop: 16 }}>
            <AccountListTab />
          </div>
        </TabPane>
        <TabPane tab="系统配置" itemKey="settings">
          <div style={{ paddingTop: 16 }}>
            <SystemConfigTab />
          </div>
        </TabPane>
        <TabPane tab="渠道管理" itemKey="channels">
          <div style={{ paddingTop: 16 }}>
            <LazyTab><ChannelsTab /></LazyTab>
          </div>
        </TabPane>
        <TabPane tab="令牌管理" itemKey="tokens">
          <div style={{ paddingTop: 16 }}>
            <LazyTab><TokensTab /></LazyTab>
          </div>
        </TabPane>
        <TabPane tab="使用日志" itemKey="logs">
          <div style={{ paddingTop: 16 }}>
            <LazyTab><UsageLogsTab /></LazyTab>
          </div>
        </TabPane>
        <TabPane tab="模型管理" itemKey="models">
          <div style={{ paddingTop: 16 }}>
            <LazyTab><ModelsTab /></LazyTab>
          </div>
        </TabPane>
        <TabPane tab="网关设置" itemKey="gateway">
          <div style={{ paddingTop: 16 }}>
            <LazyTab><GatewaySettingsTab /></LazyTab>
          </div>
        </TabPane>
      </Tabs>
    </div>
  )
}
