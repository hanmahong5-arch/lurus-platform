import React from 'react'
import { Button, Card, Tag, Typography } from '@douyinfe/semi-ui'
import { useNavigate } from 'react-router-dom'
import { useStore } from '../../store'
import LurusBadge from '../../components/LurusBadge'

const { Title, Text } = Typography

// Product definitions — URLs are environment-specific.
const PRODUCTS = [
  {
    id:          'llm-api',
    name:        'LLM API',
    description: '高性能大模型 API 接入，按量计费',
    icon:        '🔵',
    color:       '#1677ff',
    url:         'https://newapi.lurus.cn',
  },
  {
    id:          'quant-trading',
    name:        'AI 量化',
    description: '智能量化交易策略平台',
    icon:        '🟡',
    color:       '#faad14',
    url:         'https://gushen.lurus.cn',
  },
  {
    id:          'webmail',
    name:        '邮箱服务',
    description: '企业级邮件托管，域名邮箱',
    icon:        '🟣',
    color:       '#722ed1',
    url:         'https://mail.lurus.cn',
  },
]

// Quick action shortcuts shown below the product cards.
const QUICK_ACTIONS = [
  { label: '钱包充值', path: '/topup',         icon: '💰' },
  { label: '推荐朋友', path: '/wallet',         icon: '🎁' },
  { label: '订阅管理', path: '/subscriptions',  icon: '📋' },
  { label: '兑换码',   path: '/redeem',         icon: '🎫' },
]

export default function HubPage() {
  const navigate      = useNavigate()
  const { account, subscriptions } = useStore()

  // Build a map of productId → active subscription for quick lookup.
  const activeSubs = Object.fromEntries(
    subscriptions
      .filter((s) => s.status === 'active' || s.status === 'grace' || s.status === 'trial')
      .map((s) => [s.product_id, s])
  )

  return (
    <div>
      <Title heading={3} style={{ marginBottom: 8 }}>我的 AI 产品</Title>
      <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>
        欢迎回来，{account?.display_name || account?.nickname || '用户'}
      </Text>

      {/* Product cards */}
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 32 }}>
        {PRODUCTS.map((product) => {
          const sub = activeSubs[product.id]
          const isSubscribed = !!sub

          return (
            <Card
              key={product.id}
              shadows="hover"
              style={{
                flex:        '1 1 200px',
                minWidth:    200,
                maxWidth:    280,
                borderTop:   `3px solid ${product.color}`,
                cursor:      'pointer',
              }}
            >
              <div style={{ marginBottom: 12 }}>
                <span style={{ fontSize: 32 }}>{product.icon}</span>
              </div>

              <div style={{ fontWeight: 600, fontSize: 16, marginBottom: 4 }}>
                {product.name}
              </div>
              <Text type="secondary" size="small" style={{ display: 'block', marginBottom: 12 }}>
                {product.description}
              </Text>

              <div style={{ marginBottom: 16, minHeight: 24 }}>
                {isSubscribed ? (
                  <LurusBadge
                    productId={product.id}
                    planCode={sub.plan_code}
                    size="sm"
                  />
                ) : (
                  <Tag color="grey" size="small">未订阅</Tag>
                )}
              </div>

              {isSubscribed ? (
                <Button
                  type="primary"
                  size="small"
                  block
                  onClick={() => window.open(product.url, '_blank', 'noopener')}
                  style={{ background: product.color, borderColor: product.color }}
                >
                  进入
                </Button>
              ) : (
                <Button
                  size="small"
                  block
                  onClick={() => navigate('/subscriptions')}
                >
                  了解订阅
                </Button>
              )}
            </Card>
          )
        })}
      </div>

      {/* Quick actions */}
      <Card title="快捷功能" shadows="always">
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          {QUICK_ACTIONS.map(({ label, path, icon }) => (
            <Button
              key={path}
              size="large"
              onClick={() => navigate(path)}
              style={{ minWidth: 110 }}
              icon={<span style={{ marginRight: 4 }}>{icon}</span>}
            >
              {label}
            </Button>
          ))}
        </div>
      </Card>
    </div>
  )
}
