import React, { useState, useEffect, useCallback } from 'react'
import {
  Card, Table, Button, Modal, Input, InputNumber, Select, Switch,
  Toast, Tag, Typography, Popconfirm, Space, TextArea,
} from '@douyinfe/semi-ui'
import newapi from '../../api/newapi'

const { Text } = Typography

const CHANNEL_OPTIONS = [
  { value: 1, color: 'green', label: 'OpenAI' },
  { value: 14, color: 'indigo', label: 'Anthropic Claude' },
  { value: 33, color: 'indigo', label: 'AWS Claude' },
  { value: 41, color: 'blue', label: 'Vertex AI' },
  { value: 3, color: 'teal', label: 'Azure OpenAI' },
  { value: 24, color: 'orange', label: 'Google Gemini' },
  { value: 43, color: 'blue', label: 'DeepSeek' },
  { value: 17, color: 'orange', label: '阿里通义千问' },
  { value: 15, color: 'blue', label: '百度文心千帆' },
  { value: 46, color: 'blue', label: '百度文心千帆V2' },
  { value: 18, color: 'blue', label: '讯飞星火认知' },
  { value: 26, color: 'purple', label: '智谱 GLM-4V' },
  { value: 25, color: 'green', label: 'Moonshot' },
  { value: 20, color: 'green', label: 'OpenRouter' },
  { value: 34, color: 'purple', label: 'Cohere' },
  { value: 35, color: 'green', label: 'MiniMax' },
  { value: 40, color: 'purple', label: 'SiliconCloud' },
  { value: 42, color: 'blue', label: 'Mistral AI' },
  { value: 45, color: 'blue', label: '火山方舟/豆包' },
  { value: 48, color: 'blue', label: 'xAI' },
  { value: 49, color: 'blue', label: 'Coze' },
  { value: 4, color: 'grey', label: 'Ollama' },
  { value: 37, color: 'teal', label: 'Dify' },
  { value: 39, color: 'grey', label: 'Cloudflare' },
  { value: 8, color: 'pink', label: '自定义渠道' },
]

const CHANNEL_TYPE_MAP = Object.fromEntries(CHANNEL_OPTIONS.map(o => [o.value, o]))

const STATUS_MAP = {
  1: { text: '已启用', color: 'green' },
  2: { text: '已禁用', color: 'red' },
  3: { text: '自动禁用', color: 'yellow' },
}

const EMPTY_FORM = {
  name: '', type: 1, key: '', base_url: '', models: '',
  group: 'default', weight: 1, priority: 0,
}

export default function ChannelsTab() {
  const [channels, setChannels] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [modalVisible, setModalVisible] = useState(false)
  const [editData, setEditData] = useState(null)
  const [form, setForm] = useState({ ...EMPTY_FORM })
  const [saving, setSaving] = useState(false)
  const [testingId, setTestingId] = useState(null)
  const pageSize = 20

  const fetchChannels = useCallback(async (p = page) => {
    setLoading(true)
    try {
      const res = await newapi.get('/channel/', { params: { p, page_size: pageSize } })
      setChannels(res.data.data ?? [])
      setTotal(res.data.total ?? 0)
    } catch (err) {
      Toast.error(err.message || '加载渠道失败')
    } finally {
      setLoading(false)
    }
  }, [page])

  useEffect(() => { fetchChannels(page) }, [page])

  function openCreate() {
    setEditData(null)
    setForm({ ...EMPTY_FORM })
    setModalVisible(true)
  }

  function openEdit(row) {
    setEditData(row)
    setForm({
      name: row.name ?? '',
      type: row.type ?? 1,
      key: row.key ?? '',
      base_url: row.base_url ?? '',
      models: row.models ?? '',
      group: row.group ?? 'default',
      weight: row.weight ?? 1,
      priority: row.priority ?? 0,
    })
    setModalVisible(true)
  }

  async function handleSave() {
    if (!form.name || !form.key) {
      Toast.warning('名称和密钥不能为空')
      return
    }
    setSaving(true)
    try {
      if (editData) {
        await newapi.put('/channel/', { ...form, id: editData.id })
        Toast.success('渠道已更新')
      } else {
        await newapi.post('/channel/', form)
        Toast.success('渠道已创建')
      }
      setModalVisible(false)
      fetchChannels(page)
    } catch (err) {
      Toast.error(err.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id) {
    try {
      await newapi.delete(`/channel/${id}`)
      Toast.success('已删除')
      fetchChannels(page)
    } catch (err) {
      Toast.error(err.message || '删除失败')
    }
  }

  async function handleToggleStatus(row) {
    const newStatus = row.status === 1 ? 2 : 1
    try {
      await newapi.put('/channel/', { ...row, status: newStatus })
      Toast.success(newStatus === 1 ? '已启用' : '已禁用')
      fetchChannels(page)
    } catch (err) {
      Toast.error(err.message || '操作失败')
    }
  }

  async function handleTest(id) {
    setTestingId(id)
    try {
      const res = await newapi.get(`/channel/test/${id}`)
      const time = res.data?.time
      Toast.success(`测试通过${time ? `，响应 ${time}ms` : ''}`)
      fetchChannels(page)
    } catch (err) {
      Toast.error(err.message || '测试失败')
    } finally {
      setTestingId(null)
    }
  }

  function renderType(type) {
    const opt = CHANNEL_TYPE_MAP[type]
    if (!opt) return <Tag>{type}</Tag>
    return <Tag color={opt.color}>{opt.label}</Tag>
  }

  function renderStatus(status) {
    const s = STATUS_MAP[status] || { text: '未知', color: 'grey' }
    return <Tag color={s.color}>{s.text}</Tag>
  }

  function renderResponseTime(ms) {
    if (!ms) return <Text type="tertiary">-</Text>
    let color = 'green'
    if (ms > 5000) color = 'red'
    else if (ms > 3000) color = 'warning'
    else if (ms > 1000) color = 'lime'
    return <Text style={{ color }}>{(ms / 1000).toFixed(2)}s</Text>
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', width: 160 },
    { title: '类型', dataIndex: 'type', width: 140, render: renderType },
    { title: '状态', dataIndex: 'status', width: 100, render: renderStatus },
    {
      title: '模型数',
      dataIndex: 'models',
      width: 80,
      render: (v) => <Text>{v ? v.split(',').length : 0}</Text>,
    },
    {
      title: '余额',
      dataIndex: 'balance',
      width: 100,
      render: (v) => <Text>{typeof v === 'number' ? `$${v.toFixed(2)}` : '-'}</Text>,
    },
    { title: '响应时间', dataIndex: 'response_time', width: 100, render: renderResponseTime },
    { title: '优先级', dataIndex: 'priority', width: 70 },
    { title: '权重', dataIndex: 'weight', width: 70 },
    {
      title: '操作',
      width: 220,
      render: (_, row) => (
        <Space>
          <Button size="small" onClick={() => openEdit(row)}>编辑</Button>
          <Button
            size="small"
            type="tertiary"
            loading={testingId === row.id}
            onClick={() => handleTest(row.id)}
          >
            测试
          </Button>
          <Switch
            size="small"
            checked={row.status === 1}
            onChange={() => handleToggleStatus(row)}
          />
          <Popconfirm title="确定删除此渠道？" onConfirm={() => handleDelete(row.id)}>
            <Button size="small" type="danger">删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 16 }}>
        <Button type="primary" onClick={openCreate}>添加渠道</Button>
      </div>

      <Card>
        <Table
          columns={columns}
          dataSource={channels}
          loading={loading}
          rowKey="id"
          pagination={{
            total,
            currentPage: page,
            pageSize,
            onPageChange: setPage,
          }}
          scroll={{ x: 1100 }}
        />
      </Card>

      <Modal
        title={editData ? '编辑渠道' : '添加渠道'}
        visible={modalVisible}
        onOk={handleSave}
        onCancel={() => setModalVisible(false)}
        okText="保存"
        confirmLoading={saving}
        width={640}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>名称</Text>
            <Input value={form.name} onChange={(v) => setForm(f => ({ ...f, name: v }))} />
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>类型</Text>
            <Select
              value={form.type}
              onChange={(v) => setForm(f => ({ ...f, type: v }))}
              style={{ width: '100%' }}
              optionList={CHANNEL_OPTIONS.map(o => ({ value: o.value, label: o.label }))}
            />
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>密钥 (Key)</Text>
            <TextArea
              value={form.key}
              onChange={(v) => setForm(f => ({ ...f, key: v }))}
              autosize={{ minRows: 2, maxRows: 6 }}
              placeholder="多个密钥用换行分隔"
            />
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>代理地址 (Base URL)</Text>
            <Input
              value={form.base_url}
              onChange={(v) => setForm(f => ({ ...f, base_url: v }))}
              placeholder="留空使用默认地址"
            />
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>模型 (逗号分隔)</Text>
            <TextArea
              value={form.models}
              onChange={(v) => setForm(f => ({ ...f, models: v }))}
              autosize={{ minRows: 2, maxRows: 4 }}
              placeholder="gpt-4o,claude-3-5-sonnet"
            />
          </div>
          <div style={{ display: 'flex', gap: 16 }}>
            <div style={{ flex: 1 }}>
              <Text style={{ display: 'block', marginBottom: 4 }}>分组</Text>
              <Input value={form.group} onChange={(v) => setForm(f => ({ ...f, group: v }))} />
            </div>
            <div style={{ flex: 1 }}>
              <Text style={{ display: 'block', marginBottom: 4 }}>优先级</Text>
              <InputNumber
                value={form.priority}
                onChange={(v) => setForm(f => ({ ...f, priority: v }))}
                style={{ width: '100%' }}
              />
            </div>
            <div style={{ flex: 1 }}>
              <Text style={{ display: 'block', marginBottom: 4 }}>权重</Text>
              <InputNumber
                value={form.weight}
                onChange={(v) => setForm(f => ({ ...f, weight: v }))}
                min={1}
                style={{ width: '100%' }}
              />
            </div>
          </div>
        </div>
      </Modal>
    </div>
  )
}
