import React, { useEffect, useState } from 'react'
import {
  Card, Typography, Table, Tag, Toast, Button, Modal, Form,
  Input, Select, InputNumber, Banner, Space, Popconfirm,
} from '@douyinfe/semi-ui'
import axios from 'axios'

const { Title, Paragraph, Text } = Typography

const adminClient = axios.create({ baseURL: '/admin/v1', timeout: 15000 })
adminClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('lurus_token') || localStorage.getItem('token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

const PURPOSE_LABELS = {
  login_ui: 'Login UI',
  mcp:      'MCP 服务',
  external: '外部集成',
  admin:    '内部管理',
}

const STATUS_LABELS = {
  creating: { color: 'amber',  text: '创建中' },
  active:   { color: 'green',  text: '生效中' },
  failed:   { color: 'red',    text: '失败' },
  revoked:  { color: 'grey',   text: '已撤销' },
}

export default function ApiKeysTab() {
  const [loading, setLoading] = useState(false)
  const [apiKeys, setApiKeys] = useState([])
  const [errMsg, setErrMsg] = useState('')

  // Create modal state
  const [showCreate, setShowCreate] = useState(false)
  const [creating, setCreating] = useState(false)
  const [form, setForm] = useState({ name: '', display_name: '', purpose: 'mcp', expiration_days: 365 })

  // Token reveal modal — token 只在创建/rotate 后显示一次
  const [revealedToken, setRevealedToken] = useState(null) // { name, token }

  // Rotate / revoke per-row in-flight state
  const [rotatingName, setRotatingName] = useState('')
  const [revokingName, setRevokingName] = useState('')

  async function fetchKeys() {
    setLoading(true)
    setErrMsg('')
    try {
      const res = await adminClient.get('/api-keys')
      setApiKeys(res.data?.api_keys ?? [])
    } catch (err) {
      const msg = err?.response?.data?.error || err.message
      setErrMsg(msg)
      Toast.error('加载应用密钥列表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchKeys() }, [])

  async function handleCreate() {
    if (!form.name || !form.display_name) {
      Toast.warning('请填写 名称 和 显示名')
      return
    }
    setCreating(true)
    try {
      const res = await adminClient.post('/api-keys', form)
      // 201 created
      setRevealedToken({ name: res.data.name, token: res.data.token })
      setShowCreate(false)
      setForm({ name: '', display_name: '', purpose: 'mcp', expiration_days: 365 })
      fetchKeys()
    } catch (err) {
      const status = err?.response?.status
      const data = err?.response?.data
      if (status === 409 && data?.id) {
        // 幂等冲突：已存在
        Toast.info(`同名密钥 "${form.name}" 已存在 (状态: ${data.status}). 如要换 token, 用 "换发" 按钮.`)
        setShowCreate(false)
        fetchKeys()
      } else {
        Toast.error(data?.error || data?.hint || err.message)
      }
    } finally {
      setCreating(false)
    }
  }

  async function handleRotate(row) {
    setRotatingName(row.name)
    try {
      const res = await adminClient.post(`/api-keys/${row.name}/rotate`)
      setRevealedToken({ name: res.data.name, token: res.data.token })
      fetchKeys()
    } catch (err) {
      Toast.error(err?.response?.data?.error || err.message)
    } finally {
      setRotatingName('')
    }
  }

  async function handleRevoke(row) {
    setRevokingName(row.name)
    try {
      await adminClient.delete(`/api-keys/${row.name}`)
      Toast.success(`已撤销 ${row.display_name}`)
      fetchKeys()
    } catch (err) {
      Toast.error(err?.response?.data?.error || err.message)
    } finally {
      setRevokingName('')
    }
  }

  const columns = [
    { title: '名称', dataIndex: 'name', width: 160,
      render: (v) => <code style={{ fontSize: 13 }}>{v}</code> },
    { title: '显示名', dataIndex: 'display_name', width: 200 },
    { title: '用途', dataIndex: 'purpose', width: 110,
      render: (v) => <Tag color="blue">{PURPOSE_LABELS[v] || v}</Tag> },
    { title: '状态', dataIndex: 'status', width: 100,
      render: (v) => {
        const s = STATUS_LABELS[v] || { color: 'grey', text: v }
        return <Tag color={s.color}>{s.text}</Tag>
      } },
    { title: '过期', dataIndex: 'expires_at', width: 170,
      render: (v) => v ? new Date(v).toLocaleString('zh-CN') : <Text type="tertiary">永不</Text> },
    { title: '创建时间', dataIndex: 'created_at', width: 170,
      render: (v) => new Date(v).toLocaleString('zh-CN') },
    {
      title: '操作', width: 200,
      render: (_, row) => (
        <Space>
          <Popconfirm
            title={`换发 "${row.display_name}"?`}
            content="旧 token 会立即失效，新 token 创建后只显示一次。"
            onConfirm={() => handleRotate(row)}
          >
            <Button size="small" loading={rotatingName === row.name} disabled={row.status !== 'active'}>
              换发
            </Button>
          </Popconfirm>
          <Popconfirm
            title={`撤销 "${row.display_name}"?`}
            content="会删除 Zitadel 端的服务账号，不可恢复（同名重建会保留审计历史）。"
            okType="danger"
            onConfirm={() => handleRevoke(row)}
          >
            <Button size="small" type="danger" loading={revokingName === row.name} disabled={row.status === 'revoked'}>
              撤销
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 16 }}>
        <div>
          <Title heading={5}>应用密钥</Title>
          <Paragraph type="secondary">
            为 Login UI / MCP / 外部集成生成的 API 密钥。底层是 Zitadel 服务账号 + PAT，
            但运维者无需进 Zitadel console — 这里就能完成 创建 / 换发 / 撤销 全流程。
          </Paragraph>
        </div>
        <Button type="primary" onClick={() => setShowCreate(true)}>+ 新建密钥</Button>
      </div>

      {errMsg && (
        <Banner type="danger" description={errMsg} closeIcon={null} style={{ marginBottom: 16 }} />
      )}

      <Card>
        <Table columns={columns} dataSource={apiKeys} loading={loading} rowKey="id" pagination={false}
          empty={loading ? '加载中…' : '暂无密钥'} />
      </Card>

      {/* 创建弹窗 */}
      <Modal title="新建应用密钥" visible={showCreate} onCancel={() => setShowCreate(false)}
        footer={
          <>
            <Button onClick={() => setShowCreate(false)}>取消</Button>
            <Button type="primary" loading={creating} onClick={handleCreate}>创建</Button>
          </>
        }>
        <Form labelPosition="left" labelWidth={100}>
          <Form.Slot label="名称">
            <Input value={form.name} onChange={(v) => setForm({ ...form, name: v })}
              placeholder="login-ui (3-64 chars, 小写字母/数字/dash/underscore)" />
            <Text size="small" type="tertiary">用作幂等键 — 同名二次创建返回已有项</Text>
          </Form.Slot>
          <Form.Slot label="显示名">
            <Input value={form.display_name} onChange={(v) => setForm({ ...form, display_name: v })}
              placeholder="Lurus Login UI" />
          </Form.Slot>
          <Form.Slot label="用途">
            <Select value={form.purpose} onChange={(v) => setForm({ ...form, purpose: v })} style={{ width: '100%' }}>
              {Object.entries(PURPOSE_LABELS).map(([k, label]) => (
                <Select.Option key={k} value={k}>{label} ({k})</Select.Option>
              ))}
            </Select>
          </Form.Slot>
          <Form.Slot label="过期">
            <InputNumber value={form.expiration_days} onChange={(v) => setForm({ ...form, expiration_days: v })}
              min={0} max={3650} style={{ width: '100%' }} suffix="天" />
            <Text size="small" type="tertiary">0 = 永不过期。生产建议 ≤ 365 天，到期前换发。</Text>
          </Form.Slot>
        </Form>
      </Modal>

      {/* Token 显示弹窗（仅显示一次）*/}
      <Modal title={`✅ 密钥已创建: ${revealedToken?.name}`} visible={!!revealedToken}
        onCancel={() => setRevealedToken(null)}
        footer={<Button type="primary" onClick={() => setRevealedToken(null)}>我已保存</Button>}
        closeOnEsc={false} maskClosable={false} width={560}>
        <Banner type="warning" description="⚠️ 此 token 只显示一次。关闭弹窗后无法再查看。请立即复制保存到 K8s secret 或密码管理器。"
          closeIcon={null} style={{ marginBottom: 12 }} />
        <Card style={{ background: '#f5f5f5' }}>
          <code style={{ wordBreak: 'break-all', fontSize: 13, fontFamily: 'monospace' }}>
            {revealedToken?.token}
          </code>
        </Card>
        <Button style={{ marginTop: 12 }} onClick={() => {
          navigator.clipboard.writeText(revealedToken?.token || '')
          Toast.success('已复制到剪贴板')
        }}>📋 复制</Button>
      </Modal>
    </div>
  )
}
