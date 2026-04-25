import React, { useEffect, useState } from 'react'
import {
  Card, Typography, Table, Tag, Toast, Button, Tooltip, Banner,
} from '@douyinfe/semi-ui'
import axios from 'axios'

const { Title, Text, Paragraph } = Typography

// Dedicated axios instance for /admin/v1 — mirrors the main Admin page
// so auth interception + base URL are handled consistently.
const adminClient = axios.create({ baseURL: '/admin/v1', timeout: 15000 })
adminClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('lurus_token') || localStorage.getItem('token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

// Flattens apps × environments into a single row list for the table,
// keeping the 1:N relationship visible through the Application column.
// Doing this at the view layer lets the API stay grouped (handy for
// future JSON consumers) while the UI renders flat rows.
function flatten(apps) {
  const rows = []
  for (const app of apps || []) {
    for (const env of app.environments || []) {
      rows.push({
        key: `${app.name}:${env.env}`,
        appName: app.name,
        displayName: app.display_name || app.name,
        enabled: app.enabled,
        appType: app.app_type,
        authMethod: app.auth_method,
        env: env.env,
        domain: env.domain,
        redirectUri: env.redirect_uri,
        secretNamespace: env.secret_namespace,
        secretName: env.secret_name,
        zitadelAppId: env.zitadel_app_id || '',
        clientIdPreview: env.client_id_preview || '',
      })
    }
  }
  return rows
}

export default function AppsTab() {
  const [loading, setLoading] = useState(false)
  const [view, setView] = useState(null)
  const [errMsg, setErrMsg] = useState('')

  async function fetchApps() {
    setLoading(true)
    setErrMsg('')
    try {
      const res = await adminClient.get('/apps')
      setView(res.data)
    } catch (err) {
      const msg = err?.response?.data?.message || err?.response?.data?.error || err.message
      setErrMsg(msg)
      Toast.error('加载应用注册表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchApps() }, [])

  const rows = flatten(view?.apps)

  const columns = [
    {
      title: '应用',
      dataIndex: 'displayName',
      width: 160,
      render: (_, row) => (
        <div>
          <div style={{ fontWeight: 500 }}>{row.displayName}</div>
          <Text size="small" type="tertiary">{row.appName}</Text>
        </div>
      ),
    },
    {
      title: '环境',
      dataIndex: 'env',
      width: 80,
      render: (v) => <Tag>{v}</Tag>,
    },
    {
      title: '状态',
      width: 90,
      render: (_, row) => {
        if (!row.enabled) return <Tag color="grey">已禁用</Tag>
        if (!view?.live_sync) return <Tag color="orange">YAML</Tag>
        if (row.zitadelAppId) return <Tag color="green">已注册</Tag>
        return <Tag color="red">未注册</Tag>
      },
    },
    {
      title: '域名',
      dataIndex: 'domain',
      render: (v) => <a href={`https://${v}`} target="_blank" rel="noreferrer">{v}</a>,
    },
    {
      title: 'Client ID',
      dataIndex: 'clientIdPreview',
      width: 160,
      render: (v, row) => {
        if (!row.enabled) return <Text type="tertiary">—</Text>
        if (!v) return <Text type="warning" size="small">（待 reconcile）</Text>
        return (
          <Tooltip content={`Zitadel App ID: ${row.zitadelAppId}`}>
            <Text code style={{ fontSize: 12 }}>{v}</Text>
          </Tooltip>
        )
      },
    },
    {
      title: '授权方式',
      width: 120,
      render: (_, row) => (
        <div>
          <div><Text size="small">{row.appType}</Text></div>
          <Text type="tertiary" size="small">
            {row.authMethod === 'none' ? 'PKCE' : 'confidential'}
          </Text>
        </div>
      ),
    },
    {
      title: 'Secret',
      width: 200,
      render: (_, row) => (
        <div>
          <div><Text size="small" code>{row.secretName}</Text></div>
          <Text type="tertiary" size="small">ns: {row.secretNamespace}</Text>
        </div>
      ),
    },
  ]

  return (
    <div>
      <Card style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div style={{ maxWidth: 680 }}>
            <Title heading={5} style={{ marginBottom: 4 }}>应用注册表</Title>
            <Paragraph type="tertiary" size="small">
              声明式 OIDC 应用清单（<Text code>config/apps.yaml</Text>）与 Zitadel 实际状态的汇总。
              新增应用请提交 PR 修改 <Text code>apps.yaml</Text> — reconciler 会在下一轮（≤5 分钟）
              自动创建 Zitadel OIDC 应用、写入 K8s Secret、触发部署重启。
              {view && (
                <>
                  {' '}当前管理 <Text strong>{rows.length}</Text> 个 (app, env) 对，
                  项目 <Text code>{view.project}</Text>
                  {!view.live_sync && (
                    <Text type="warning">（Zitadel 临时不可达，仅显示 YAML 声明）</Text>
                  )}
                </>
              )}
            </Paragraph>
          </div>
          <Button onClick={fetchApps} loading={loading}>刷新</Button>
        </div>
      </Card>

      {errMsg && (
        <Banner
          type="danger"
          description={errMsg}
          closeIcon={null}
          style={{ marginBottom: 16 }}
        />
      )}

      <Card>
        <Table
          columns={columns}
          dataSource={rows}
          loading={loading}
          rowKey="key"
          pagination={false}
          empty="暂无注册应用 — 在 config/apps.yaml 里声明一个。"
        />
      </Card>
    </div>
  )
}
