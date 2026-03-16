import React from 'react'
import './badge.css'

// Product → diamond color mapping
const PRODUCT_MAP = {
  'llm-api':       { label: '蓝钻', color: '#1677ff', emoji: '💙' },
  'quant-trading': { label: '金钻', color: '#faad14', emoji: '💛' },
  'webmail':       { label: '紫钻', color: '#722ed1', emoji: '💜' },
}

const DEFAULT_PRODUCT = { label: '钻石', color: '#8c8c8c', emoji: '🔘' }

// Plan code → CSS modifier
const PLAN_CLASS = {
  basic:      'badge-basic',
  pro:        'badge-pro',
  enterprise: 'badge-enterprise',
}

/**
 * LurusBadge renders a colored diamond badge for a product subscription.
 * @param {{ productId: string, planCode: string, size?: 'sm' | 'md' | 'lg' }} props
 */
export default function LurusBadge({ productId, planCode, size = 'md' }) {
  if (!planCode || planCode === 'free') return null

  const product = PRODUCT_MAP[productId] ?? DEFAULT_PRODUCT
  const planCls = PLAN_CLASS[planCode] ?? 'badge-basic'

  return (
    <span
      className={`lurus-badge ${planCls} badge-${size}`}
      style={{ '--badge-color': product.color }}
      title={`${product.label} ${planCode}`}
    >
      <span className="badge-gem">{product.emoji}</span>
      <span className="badge-label">{product.label}</span>
    </span>
  )
}
