import React, { useEffect, useState } from 'react'
import {
  Card, Typography, Table, Tag, Toast, Button, Tooltip, Banner, Modal,
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
  // rotatingKey is the row key (`${app}:${env}`) currently mid-flight.
  // Used to disable the per-row button during the request so a double
  // click can't double-rotate. Centralised here rather than per-row
  // local state because the confirmation Modal lives at this level.
  const [rotatingKey, setRotatingKey] = useState('')

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

  // rotateSecret POSTs to /admin/v1/apps/:name/:env/rotate-secret. The
  // confirmation step is wrapped in Modal.confirm to enforce a deliberate
  // operator action — rotation invalidates the previous secret server-
  // side immediately, so a misclick would page on-call.
  function rotateSecret(row) {
    Modal.confirm({
      title: '确认轮转 client_secret',
      icon: null,
      content: (
        <div>
          <p>
            目标: <Text strong code>{row.appName}</Text> / <Text strong code>{row.env}</Text>
          </p>
          <p style={{ marginTop: 8 }}>
            轮转后旧密钥将立即失效。新密钥会写入 K8s Secret <Text code>{row.secretName}</Text>，
            随后 deployment 会被滚动重启。请在低峰期执行。
          </p>
        </div>
      ),
      okText: '执行轮转',
      cancelText: '取消',
      okButtonProps: { type: 'danger' },
      onOk: async () => {
        setRotatingKey(row.key)
        try {
          const res = await adminClient.post(
            `/apps/${encodeURIComponent(row.appName)}/${encodeURIComponent(row.env)}/rotate-secret`,
          )
          const next = res.data?.next_due_at
          Toast.success({
            content: `已轮转 ${row.appName}/${row.env}`
              + (next ? ` · 下次到期 ${new Date(next).toLocaleString()}` : ''),
            duration: 5,
          })
          // Refresh so any client_id_preview / status changes show up.
          fetchApps()
        } catch (err) {
          const msg = err?.response?.data?.message || err?.response?.data?.error || err.message
          Toast.error({ content: `轮转失败: ${msg}`, duration: 6 })
        } finally {
          setRotatingKey('')
        }
      },
    })
  }

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
    {
      title: '操作',
      width: 110,
      render: (_, row) => {
        // Rotation only applies to confidential clients — PKCE apps
        // have no client_secret to rotate. Render a quiet placeholder
        // so the column stays aligned across rows.
        if (row.authMethod !== 'basic') {
          return <Text type="tertiary" size="small">—</Text>
        }
        if (!row.enabled) {
          return <Text type="tertiary" size="small">—</Text>
        }
        return (
          <Tooltip content="生成新的 client_secret 并写入 K8s Secret，然后滚动重启 deployment">
            <Button
              size="small"
              type="warning"
              loading={rotatingKey === row.key}
              disabled={!!rotatingKey && rotatingKey !== row.key}
              onClick={() => rotateSecret(row)}
            >
              轮转密钥
            </Button>
          </Tooltip>
        )
      },
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
