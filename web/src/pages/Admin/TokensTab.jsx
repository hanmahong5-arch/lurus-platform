import React, { useState, useEffect, useCallback } from 'react'
import {
  Card, Table, Button, Modal, Input, InputNumber, Switch,
  Toast, Tag, Typography, Popconfirm, Space, DatePicker,
} from '@douyinfe/semi-ui'
import newapi from '../../api/newapi'

const { Text } = Typography

const STATUS_MAP = {
  1: { text: '已启用', color: 'green' },
  2: { text: '已禁用', color: 'red' },
  3: { text: '已过期', color: 'grey' },
  4: { text: '已耗尽', color: 'yellow' },
}

const EMPTY_FORM = {
  name: '', remain_quota: 500000, expired_time: -1,
  unlimited_quota: false, models: '', subnet: '',
}

function formatQuota(v) {
  if (v === undefined || v === null) return '-'
  if (v >= 1000000) return `${(v / 1000000).toFixed(1)}M`
  if (v >= 1000) return `${(v / 1000).toFixed(1)}K`
  return String(v)
}

function maskKey(key) {
  if (!key || key.length < 10) return key || ''
  return key.slice(0, 6) + '...' + key.slice(-4)
}

export default function TokensTab() {
  const [tokens, setTokens] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [modalVisible, setModalVisible] = useState(false)
  const [editData, setEditData] = useState(null)
  const [form, setForm] = useState({ ...EMPTY_FORM })
  const [saving, setSaving] = useState(false)
  const pageSize = 20

  const fetchTokens = useCallback(async (p = page) => {
    setLoading(true)
    try {
      const res = await newapi.get('/token/', { params: { p, page_size: pageSize } })
      setTokens(res.data.data ?? [])
      setTotal(res.data.total ?? 0)
    } catch (err) {
      Toast.error(err.message || '加载令牌失败')
    } finally {
      setLoading(false)
    }
  }, [page])

  useEffect(() => { fetchTokens(page) }, [page])

  function openCreate() {
    setEditData(null)
    setForm({ ...EMPTY_FORM })
    setModalVisible(true)
  }

  function openEdit(row) {
    setEditData(row)
    setForm({
      name: row.name ?? '',
      remain_quota: row.remain_quota ?? 500000,
      expired_time: row.expired_time ?? -1,
      unlimited_quota: row.unlimited_quota ?? false,
      models: row.models ?? '',
      subnet: row.subnet ?? '',
    })
    setModalVisible(true)
  }

  async function handleSave() {
    if (!form.name) {
      Toast.warning('名称不能为空')
      return
    }
    setSaving(true)
    try {
      if (editData) {
        await newapi.put('/token/', { ...form, id: editData.id })
        Toast.success('令牌已更新')
      } else {
        const res = await newapi.post('/token/', form)
        const key = res.data?.data?.key
        if (key) {
          Modal.info({
            title: '令牌已创建',
            content: (
              <div>
                <Text>请妥善保存此密钥，关闭后无法再次查看：</Text>
                <Input value={key} readOnly style={{ marginTop: 8 }} />
                <Button
                  size="small"
                  style={{ marginTop: 8 }}
                  onClick={() => {
                    navigator.clipboard.writeText(key)
                    Toast.success('已复制')
                  }}
                >
                  复制
                </Button>
              </div>
            ),
          })
        } else {
          Toast.success('令牌已创建')
        }
      }
      setModalVisible(false)
      fetchTokens(page)
    } catch (err) {
      Toast.error(err.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id) {
    try {
      await newapi.delete(`/token/${id}`)
      Toast.success('已删除')
      fetchTokens(page)
    } catch (err) {
      Toast.error(err.message || '删除失败')
    }
  }

  async function handleToggleStatus(row) {
    const newStatus = row.status === 1 ? 2 : 1
    try {
      await newapi.put('/token/', { ...row, status: newStatus })
      Toast.success(newStatus === 1 ? '已启用' : '已禁用')
      fetchTokens(page)
    } catch (err) {
      Toast.error(err.message || '操作失败')
    }
  }

  function copyKey(key) {
    if (!key) return
    navigator.clipboard.writeText(key)
    Toast.success('已复制到剪贴板')
  }

  function renderStatus(status) {
    const s = STATUS_MAP[status] || { text: '未知', color: 'grey' }
    return <Tag color={s.color}>{s.text}</Tag>
  }

  function renderExpiry(ts) {
    if (!ts || ts === -1) return <Text type="tertiary">永不过期</Text>
    const d = new Date(ts * 1000)
    return <Text>{d.toLocaleDateString()}</Text>
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', width: 160 },
    {
      title: 'Key',
      dataIndex: 'key',
      width: 180,
      render: (key) => (
        <Space>
          <Text copyable={false}>{maskKey(key)}</Text>
          <Button size="small" type="tertiary" onClick={() => copyKey(key)}>
            复制
          </Button>
        </Space>
      ),
    },
    { title: '状态', dataIndex: 'status', width: 90, render: renderStatus },
    {
      title: '额度',
      width: 120,
      render: (_, row) => row.unlimited_quota
        ? <Tag color="green">无限</Tag>
        : <Text>{formatQuota(row.used_quota)} / {formatQuota(row.remain_quota)}</Text>,
    },
    { title: '模型限制', dataIndex: 'models', width: 150, render: (v) => v || <Text type="tertiary">不限</Text> },
    { title: '过期时间', dataIndex: 'expired_time', width: 120, render: renderExpiry },
    {
      title: '操作',
      width: 200,
      render: (_, row) => (
        <Space>
          <Button size="small" onClick={() => openEdit(row)}>编辑</Button>
          <Switch
            size="small"
            checked={row.status === 1}
            onChange={() => handleToggleStatus(row)}
          />
          <Popconfirm title="确定删除此令牌？" onConfirm={() => handleDelete(row.id)}>
            <Button size="small" type="danger">删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 16 }}>
        <Button type="primary" onClick={openCreate}>创建令牌</Button>
      </div>

      <Card>
        <Table
          columns={columns}
          dataSource={tokens}
          loading={loading}
          rowKey="id"
          pagination={{
            total,
            currentPage: page,
            pageSize,
            onPageChange: setPage,
          }}
          scroll={{ x: 1000 }}
        />
      </Card>

      <Modal
        title={editData ? '编辑令牌' : '创建令牌'}
        visible={modalVisible}
        onOk={handleSave}
        onCancel={() => setModalVisible(false)}
        okText="保存"
        confirmLoading={saving}
        width={560}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>名称</Text>
            <Input value={form.name} onChange={(v) => setForm(f => ({ ...f, name: v }))} />
          </div>
          <div style={{ display: 'flex', gap: 16, alignItems: 'center' }}>
            <div style={{ flex: 1 }}>
              <Text style={{ display: 'block', marginBottom: 4 }}>额度</Text>
              <InputNumber
                value={form.remain_quota}
                onChange={(v) => setForm(f => ({ ...f, remain_quota: v }))}
                min={0}
                disabled={form.unlimited_quota}
                style={{ width: '100%' }}
              />
            </div>
            <div>
              <Text style={{ display: 'block', marginBottom: 4 }}>无限额度</Text>
              <Switch
                checked={form.unlimited_quota}
                onChange={(v) => setForm(f => ({ ...f, unlimited_quota: v }))}
              />
            </div>
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>模型限制 (逗号分隔，留空=不限)</Text>
            <Input
              value={form.models}
              onChange={(v) => setForm(f => ({ ...f, models: v }))}
              placeholder="gpt-4o,claude-3-5-sonnet"
            />
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>IP 白名单 (CIDR，留空=不限)</Text>
            <Input
              value={form.subnet}
              onChange={(v) => setForm(f => ({ ...f, subnet: v }))}
              placeholder="10.0.0.0/8"
            />
          </div>
        </div>
      </Modal>
    </div>
  )
}
