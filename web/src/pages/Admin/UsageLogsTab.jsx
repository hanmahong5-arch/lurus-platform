import React, { useState, useEffect, useCallback } from 'react'
import {
  Card, Table, Input, Button, Toast, Tag, Typography, DatePicker, Select, Space,
} from '@douyinfe/semi-ui'
import newapi from '../../api/newapi'

const { Text, Title } = Typography

function formatTokens(v) {
  if (v === undefined || v === null) return '-'
  if (v >= 1000000) return `${(v / 1000000).toFixed(1)}M`
  if (v >= 1000) return `${(v / 1000).toFixed(1)}K`
  return String(v)
}

function formatCost(v) {
  if (v === undefined || v === null) return '-'
  return `$${(v / 500000).toFixed(4)}`
}

function formatDuration(ms) {
  if (!ms) return '-'
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  return `${ms}ms`
}

export default function UsageLogsTab() {
  const [logs, setLogs] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [stat, setStat] = useState(null)

  // Filters
  const [modelFilter, setModelFilter] = useState('')
  const [usernameFilter, setUsernameFilter] = useState('')
  const [dateRange, setDateRange] = useState(null)

  const pageSize = 20

  const fetchLogs = useCallback(async (p = page) => {
    setLoading(true)
    try {
      const params = { p, page_size: pageSize, type: 2 }
      if (modelFilter) params.model_name = modelFilter
      if (usernameFilter) params.username = usernameFilter
      if (dateRange?.[0]) params.start_timestamp = Math.floor(dateRange[0].getTime() / 1000)
      if (dateRange?.[1]) params.end_timestamp = Math.floor(dateRange[1].getTime() / 1000)

      const res = await newapi.get('/log/', { params })
      setLogs(res.data.data ?? [])
      setTotal(res.data.total ?? 0)
    } catch (err) {
      Toast.error(err.message || '加载日志失败')
    } finally {
      setLoading(false)
    }
  }, [page, modelFilter, usernameFilter, dateRange])

  const fetchStat = useCallback(async () => {
    try {
      const params = { type: 2 }
      if (dateRange?.[0]) params.start_timestamp = Math.floor(dateRange[0].getTime() / 1000)
      if (dateRange?.[1]) params.end_timestamp = Math.floor(dateRange[1].getTime() / 1000)
      const res = await newapi.get('/log/stat', { params })
      setStat(res.data.data ?? res.data)
    } catch {
      // stat is optional, don't block
    }
  }, [dateRange])

  useEffect(() => { fetchLogs(page) }, [page])

  function handleSearch() {
    setPage(1)
    fetchLogs(1)
    fetchStat()
  }

  useEffect(() => { fetchStat() }, [])

  function formatTime(ts) {
    if (!ts) return '-'
    return new Date(ts * 1000).toLocaleString()
  }

  const columns = [
    { title: '时间', dataIndex: 'created_at', width: 170, render: formatTime },
    { title: '用户', dataIndex: 'username', width: 120 },
    { title: '模型', dataIndex: 'model_name', width: 180, render: (v) => <Tag>{v}</Tag> },
    { title: 'Prompt', dataIndex: 'prompt_tokens', width: 90, render: formatTokens },
    { title: 'Completion', dataIndex: 'completion_tokens', width: 100, render: formatTokens },
    { title: '费用', dataIndex: 'quota', width: 90, render: formatCost },
    { title: '耗时', dataIndex: 'elapsed_time', width: 90, render: formatDuration },
    { title: '渠道', dataIndex: 'channel', width: 80 },
    {
      title: '状态',
      dataIndex: 'code',
      width: 70,
      render: (v) => v === 200
        ? <Tag color="green">OK</Tag>
        : <Tag color="red">{v}</Tag>,
    },
  ]

  const statCards = stat ? [
    { label: '总请求数', value: stat.request_count ?? stat.total ?? '-' },
    { label: '总 Token', value: formatTokens((stat.prompt_tokens ?? 0) + (stat.completion_tokens ?? 0)) },
    { label: '总费用', value: formatCost(stat.quota ?? 0) },
  ] : null

  return (
    <div>
      {statCards && (
        <div style={{ display: 'flex', gap: 16, marginBottom: 16 }}>
          {statCards.map((s) => (
            <Card key={s.label} style={{ flex: 1, textAlign: 'center' }}>
              <Text type="tertiary" size="small">{s.label}</Text>
              <Title heading={4} style={{ margin: '4px 0 0' }}>{s.value}</Title>
            </Card>
          ))}
        </div>
      )}

      <div style={{ display: 'flex', gap: 10, marginBottom: 16, flexWrap: 'wrap' }}>
        <DatePicker
          type="dateRange"
          value={dateRange}
          onChange={setDateRange}
          style={{ width: 260 }}
          placeholder={['开始日期', '结束日期']}
        />
        <Input
          value={modelFilter}
          onChange={setModelFilter}
          placeholder="模型名称"
          style={{ width: 180 }}
        />
        <Input
          value={usernameFilter}
          onChange={setUsernameFilter}
          placeholder="用户名"
          style={{ width: 160 }}
        />
        <Button onClick={handleSearch}>搜索</Button>
      </div>

      <Card>
        <Table
          columns={columns}
          dataSource={logs}
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
    </div>
  )
}
