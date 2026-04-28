import React, { useEffect, useState } from 'react'
import {
  Card, Typography, Table, Tag, Toast, Banner,
} from '@douyinfe/semi-ui'
import axios from 'axios'

const { Title, Paragraph } = Typography

const adminClient = axios.create({ baseURL: '/admin/v1', timeout: 15000 })
adminClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('lurus_token') || localStorage.getItem('token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

const RISK_COLORS = {
  info: 'blue',
  warn: 'amber',
  destructive: 'red',
}

const RISK_LABELS = {
  info: '信息',
  warn: '中等',
  destructive: '高危',
}

export default function OpsTab() {
  const [loading, setLoading] = useState(false)
  const [ops, setOps] = useState([])
  const [errMsg, setErrMsg] = useState('')

  async function fetchOps() {
    setLoading(true)
    setErrMsg('')
    try {
      const res = await adminClient.get('/ops')
      setOps(res.data?.ops ?? [])
    } catch (err) {
      const msg = err?.response?.data?.message || err?.response?.data?.error || err.message
      setErrMsg(msg)
      Toast.error('加载特权操作清单失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchOps() }, [])

  const columns = [
    { title: '类型', dataIndex: 'type', width: 200,
      render: (v) => <code style={{ fontSize: 13 }}>{v}</code> },
    { title: '说明', dataIndex: 'description' },
    { title: '风险', dataIndex: 'risk_level', width: 100,
      render: (v) => <Tag color={RISK_COLORS[v] || 'grey'}>{RISK_LABELS[v] || v}</Tag> },
    { title: '不可逆', dataIndex: 'destructive', width: 90,
      render: (v) => v ? <Tag color="red">是</Tag> : <Tag color="grey">否</Tag> },
    { title: 'APP 确认', dataIndex: 'delegate', width: 100,
      render: (v) => v ? <Tag color="violet">需扫码</Tag> : <Tag color="grey">直接</Tag> },
  ]

  return (
    <div>
      <Title heading={5} style={{ marginBottom: 8 }}>特权操作清单</Title>
      <Paragraph type="secondary" style={{ marginBottom: 16 }}>
        平台对外暴露的所有特权操作。<b>需扫码</b> = 通过路途 APP 生物识别确认；
        <b>直接</b> = Web 端管理员直接执行，无需手机确认。
      </Paragraph>

      {errMsg && (
        <Banner type="danger" description={errMsg} closeIcon={null} style={{ marginBottom: 16 }} />
      )}

      <Card>
        <Table
          columns={columns}
          dataSource={ops}
          loading={loading}
          rowKey="type"
          pagination={false}
          empty={loading ? '加载中…' : '暂无操作（registry 为空）'}
        />
      </Card>
    </div>
  )
}
