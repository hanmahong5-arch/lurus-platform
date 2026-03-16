import React, { useState, useEffect } from 'react'
import {
  Card, Button, Input, InputNumber, Switch, Toast, Typography, Divider,
} from '@douyinfe/semi-ui'
import newapi from '../../api/newapi'

const { Text, Title } = Typography

// Key groups for organized display.
const SECTIONS = [
  {
    title: '运营设置',
    keys: [
      { key: 'RegisterEnabled', label: '开放注册', type: 'bool' },
      { key: 'QuotaForNewUser', label: '新用户初始额度', type: 'number' },
      { key: 'QuotaForInviter', label: '邀请奖励额度', type: 'number' },
      { key: 'QuotaForInvitee', label: '被邀请奖励额度', type: 'number' },
      { key: 'TopUpLink', label: '充值链接', type: 'string' },
      { key: 'QuotaRemindThreshold', label: '额度提醒阈值', type: 'number' },
    ],
  },
  {
    title: '模型定价',
    keys: [
      { key: 'ModelPrice', label: '模型价格 (JSON)', type: 'string' },
      { key: 'CompletionPrice', label: '补全价格 (JSON)', type: 'string' },
      { key: 'GroupRatio', label: '分组倍率 (JSON)', type: 'string' },
    ],
  },
  {
    title: '速率限制',
    keys: [
      { key: 'GlobalApiRateLimitNum', label: '全局 API 每分钟限制', type: 'number' },
      { key: 'DefaultChannelRateLimitNum', label: '默认渠道每分钟限制', type: 'number' },
    ],
  },
  {
    title: '日志配置',
    keys: [
      { key: 'LogConsumeEnabled', label: '记录消费日志', type: 'bool' },
      { key: 'DisplayInCurrencyEnabled', label: '以货币显示', type: 'bool' },
    ],
  },
]

export default function GatewaySettingsTab() {
  const [options, setOptions] = useState({})
  const [edits, setEdits] = useState({})
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(false)

  async function fetchOptions() {
    setLoading(true)
    try {
      const res = await newapi.get('/option/')
      const data = res.data.data ?? res.data ?? {}
      setOptions(data)
    } catch (err) {
      Toast.error(err.message || '加载网关配置失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchOptions() }, [])

  function getValue(key) {
    if (edits[key] !== undefined) return edits[key]
    return options[key] ?? ''
  }

  function handleChange(key, value) {
    setEdits(prev => ({ ...prev, [key]: value }))
  }

  async function handleSave() {
    const keys = Object.keys(edits)
    if (keys.length === 0) {
      Toast.info('没有需要保存的更改')
      return
    }

    setSaving(true)
    try {
      // NewAPI expects PUT /option with { key: value } for each option.
      for (const key of keys) {
        let value = edits[key]
        // Convert booleans to string for newapi.
        if (typeof value === 'boolean') value = value ? 'true' : 'false'
        await newapi.put('/option/', { key, value: String(value) })
      }
      Toast.success(`已保存 ${keys.length} 项配置`)
      setEdits({})
      fetchOptions()
    } catch (err) {
      Toast.error(err.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  function renderField(field) {
    const { key, label, type } = field
    const value = getValue(key)

    if (type === 'bool') {
      const checked = value === true || value === 'true'
      return (
        <div key={key} style={{ display: 'flex', alignItems: 'center', marginBottom: 12, gap: 12 }}>
          <Text style={{ width: 200, flexShrink: 0 }}>{label}</Text>
          <Switch
            checked={checked}
            onChange={(v) => handleChange(key, v)}
          />
        </div>
      )
    }

    if (type === 'number') {
      return (
        <div key={key} style={{ display: 'flex', alignItems: 'center', marginBottom: 12, gap: 12 }}>
          <Text style={{ width: 200, flexShrink: 0 }}>{label}</Text>
          <InputNumber
            value={typeof value === 'string' ? parseInt(value, 10) || 0 : value}
            onChange={(v) => handleChange(key, v)}
            style={{ width: 200 }}
          />
        </div>
      )
    }

    // string
    return (
      <div key={key} style={{ display: 'flex', alignItems: 'center', marginBottom: 12, gap: 12 }}>
        <Text style={{ width: 200, flexShrink: 0 }}>{label}</Text>
        <Input
          value={String(value)}
          onChange={(v) => handleChange(key, v)}
          style={{ flex: 1, maxWidth: 400 }}
        />
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 800 }}>
      {SECTIONS.map((section) => (
        <Card key={section.title} title={section.title} style={{ marginBottom: 16 }}>
          {section.keys.map(renderField)}
        </Card>
      ))}

      <Button
        type="primary"
        loading={saving}
        onClick={handleSave}
        style={{ marginTop: 8 }}
      >
        保存配置
      </Button>
    </div>
  )
}
