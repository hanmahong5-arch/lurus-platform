import React, { useState, useEffect, useCallback } from 'react'
import {
  Card, Table, Button, Modal, Input, InputNumber,
  Toast, Tag, Typography, Popconfirm, Space, Select,
} from '@douyinfe/semi-ui'
import newapi from '../../api/newapi'

const { Text } = Typography

const EMPTY_FORM = {
  model_name: '', model_description: '', developer: '',
  input_price: 0, output_price: 0, tags: '',
}

export default function ModelsTab() {
  const [models, setModels] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [modalVisible, setModalVisible] = useState(false)
  const [editData, setEditData] = useState(null)
  const [form, setForm] = useState({ ...EMPTY_FORM })
  const [saving, setSaving] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const pageSize = 20

  const fetchModels = useCallback(async (p = page) => {
    setLoading(true)
    try {
      const res = await newapi.get('/models/', { params: { p, page_size: pageSize } })
      setModels(res.data.data ?? [])
      setTotal(res.data.total ?? 0)
    } catch (err) {
      Toast.error(err.message || '加载模型失败')
    } finally {
      setLoading(false)
    }
  }, [page])

  useEffect(() => { fetchModels(page) }, [page])

  function openCreate() {
    setEditData(null)
    setForm({ ...EMPTY_FORM })
    setModalVisible(true)
  }

  function openEdit(row) {
    setEditData(row)
    setForm({
      model_name: row.model_name ?? '',
      model_description: row.model_description ?? '',
      developer: row.developer ?? '',
      input_price: row.input_price ?? 0,
      output_price: row.output_price ?? 0,
      tags: row.tags ?? '',
    })
    setModalVisible(true)
  }

  async function handleSave() {
    if (!form.model_name) {
      Toast.warning('模型名称不能为空')
      return
    }
    setSaving(true)
    try {
      if (editData) {
        await newapi.put('/models/', { ...form, id: editData.id })
        Toast.success('模型已更新')
      } else {
        await newapi.post('/models/', form)
        Toast.success('模型已创建')
      }
      setModalVisible(false)
      fetchModels(page)
    } catch (err) {
      Toast.error(err.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id) {
    try {
      await newapi.delete(`/models/${id}`)
      Toast.success('已删除')
      fetchModels(page)
    } catch (err) {
      Toast.error(err.message || '删除失败')
    }
  }

  async function handleSync() {
    setSyncing(true)
    try {
      const res = await newapi.post('/models/sync_upstream')
      const count = res.data?.data?.count ?? 0
      Toast.success(`同步完成，更新 ${count} 个模型`)
      fetchModels(page)
    } catch (err) {
      Toast.error(err.message || '同步失败')
    } finally {
      setSyncing(false)
    }
  }

  function formatPrice(v) {
    if (v === undefined || v === null || v === 0) return '-'
    return `$${v.toFixed(4)}`
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '模型名', dataIndex: 'model_name', width: 220 },
    { title: '供应商', dataIndex: 'developer', width: 120 },
    { title: '输入价格', dataIndex: 'input_price', width: 100, render: formatPrice },
    { title: '输出价格', dataIndex: 'output_price', width: 100, render: formatPrice },
    {
      title: '标签',
      dataIndex: 'tags',
      width: 150,
      render: (v) => v
        ? v.split(',').map(t => <Tag key={t} style={{ margin: 2 }}>{t.trim()}</Tag>)
        : <Text type="tertiary">-</Text>,
    },
    {
      title: '操作',
      width: 160,
      render: (_, row) => (
        <Space>
          <Button size="small" onClick={() => openEdit(row)}>编辑</Button>
          <Popconfirm title="确定删除此模型？" onConfirm={() => handleDelete(row.id)}>
            <Button size="small" type="danger">删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginBottom: 16 }}>
        <Button loading={syncing} onClick={handleSync}>上游同步</Button>
        <Button type="primary" onClick={openCreate}>添加模型</Button>
      </div>

      <Card>
        <Table
          columns={columns}
          dataSource={models}
          loading={loading}
          rowKey="id"
          pagination={{
            total,
            currentPage: page,
            pageSize,
            onPageChange: setPage,
          }}
          scroll={{ x: 900 }}
        />
      </Card>

      <Modal
        title={editData ? '编辑模型' : '添加模型'}
        visible={modalVisible}
        onOk={handleSave}
        onCancel={() => setModalVisible(false)}
        okText="保存"
        confirmLoading={saving}
        width={560}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>模型名称</Text>
            <Input value={form.model_name} onChange={(v) => setForm(f => ({ ...f, model_name: v }))} />
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>供应商</Text>
            <Input value={form.developer} onChange={(v) => setForm(f => ({ ...f, developer: v }))} placeholder="e.g. OpenAI" />
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>描述</Text>
            <Input value={form.model_description} onChange={(v) => setForm(f => ({ ...f, model_description: v }))} />
          </div>
          <div style={{ display: 'flex', gap: 16 }}>
            <div style={{ flex: 1 }}>
              <Text style={{ display: 'block', marginBottom: 4 }}>输入价格 ($/1K tokens)</Text>
              <InputNumber
                value={form.input_price}
                onChange={(v) => setForm(f => ({ ...f, input_price: v }))}
                min={0}
                step={0.0001}
                style={{ width: '100%' }}
              />
            </div>
            <div style={{ flex: 1 }}>
              <Text style={{ display: 'block', marginBottom: 4 }}>输出价格 ($/1K tokens)</Text>
              <InputNumber
                value={form.output_price}
                onChange={(v) => setForm(f => ({ ...f, output_price: v }))}
                min={0}
                step={0.0001}
                style={{ width: '100%' }}
              />
            </div>
          </div>
          <div>
            <Text style={{ display: 'block', marginBottom: 4 }}>标签 (逗号分隔)</Text>
            <Input value={form.tags} onChange={(v) => setForm(f => ({ ...f, tags: v }))} placeholder="chat,vision,code" />
          </div>
        </div>
      </Modal>
    </div>
  )
}
