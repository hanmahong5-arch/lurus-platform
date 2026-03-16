import React, { useState } from 'react'
import { Popover, Avatar, Tag, Modal, Toast } from '@douyinfe/semi-ui'
import LurusBadge from '../LurusBadge'
import './avatar.css'

const VIP_COLORS = ['#8c8c8c', '#1890ff', '#52c41a', '#faad14', '#f5222d']

// LurusID profile card shown as a modal when the avatar is clicked.
function IDCard({ account, subscriptions, onClose }) {
  const lurusID  = account.lurus_id ?? '—'
  const vipLevel = account.vip_level ?? 0
  const VIP_COLORS_local = ['#8c8c8c', '#1890ff', '#52c41a', '#faad14', '#f5222d']
  const vipColor = VIP_COLORS_local[Math.min(vipLevel, VIP_COLORS_local.length - 1)]
  const initial  = (account.nickname || account.email || 'L')[0].toUpperCase()

  const profileURL = `https://lurus.cn/u/${lurusID}`

  // Generate a QR code image via a public API (no external library needed).
  const qrSrc = `https://api.qrserver.com/v1/create-qr-code/?size=140x140&data=${encodeURIComponent(profileURL)}`

  const activeSubs = subscriptions.filter(
    (s) => s.status === 'active' || s.status === 'grace' || s.status === 'trial'
  )

  function copyID() {
    navigator.clipboard.writeText(lurusID)
      .then(() => Toast.success('Lurus ID 已复制'))
      .catch(() => Toast.error('复制失败，请手动复制'))
  }

  return (
    <div style={{ textAlign: 'center', padding: '8px 0' }}>
      {/* Avatar */}
      {account.avatar_url ? (
        <Avatar size={64} src={account.avatar_url} style={{ marginBottom: 8 }} />
      ) : (
        <Avatar size={64} style={{ background: vipColor, fontSize: 28, marginBottom: 8 }}>
          {initial}
        </Avatar>
      )}

      {/* Name + VIP */}
      <div style={{ fontWeight: 700, fontSize: 18, marginBottom: 4 }}>
        {account.display_name || account.nickname || '用户'}
      </div>
      {vipLevel > 0 && (
        <Tag color="yellow" size="small" style={{ marginBottom: 8 }}>
          ★ VIP Lv.{vipLevel}
        </Tag>
      )}

      {/* LurusID */}
      <div
        style={{
          fontFamily:  'monospace',
          fontSize:    20,
          fontWeight:  700,
          color:       '#1677ff',
          letterSpacing: 2,
          marginBottom: 4,
          cursor:      'pointer',
        }}
        onClick={copyID}
        title="点击复制"
      >
        {lurusID}
      </div>
      <div style={{ fontSize: 12, color: '#8c8c8c', marginBottom: 16 }}>点击 ID 复制</div>

      {/* QR code */}
      <img
        src={qrSrc}
        alt={`QR code for ${lurusID}`}
        width={140}
        height={140}
        style={{ borderRadius: 8, border: '1px solid #f0f0f0', marginBottom: 16 }}
      />

      {/* Active subscription badges */}
      {activeSubs.length > 0 && (
        <div style={{ display: 'flex', gap: 6, justifyContent: 'center', flexWrap: 'wrap' }}>
          {activeSubs.map((sub) => (
            <LurusBadge
              key={sub.product_id}
              productId={sub.product_id}
              planCode={sub.plan_code}
              size="sm"
            />
          ))}
        </div>
      )}
    </div>
  )
}

/**
 * LurusAvatar shows a user avatar with Lurus ID.
 * Hover → popover with VIP + badges.
 * Click → modal ID card with QR code.
 *
 * @param {{ account: object, subscriptions: Array, size?: number }} props
 */
export default function LurusAvatar({ account, subscriptions = [], size = 40 }) {
  const [cardVisible, setCardVisible] = useState(false)

  if (!account) return null

  const lurusID  = account.lurus_id ?? '—'
  const vipLevel = account.vip_level ?? 0
  const vipColor = VIP_COLORS[Math.min(vipLevel, VIP_COLORS.length - 1)]
  const initial  = (account.nickname || account.email || 'L')[0].toUpperCase()

  const activeSubs = subscriptions.filter(
    (s) => s.status === 'active' || s.status === 'grace' || s.status === 'trial'
  )

  const popoverContent = (
    <div className="lurus-popover">
      <div className="popover-header">
        <span className="popover-lurus-id">{lurusID}</span>
        {vipLevel > 0 && (
          <Tag color="yellow" size="small" style={{ marginLeft: 8 }}>
            ★ VIP Lv.{vipLevel}
          </Tag>
        )}
      </div>
      <div style={{ fontSize: 12, color: '#8c8c8c', marginBottom: 6 }}>点击查看身份卡片</div>
      {activeSubs.length > 0 ? (
        <div className="popover-badges">
          {activeSubs.map((sub) => (
            <LurusBadge
              key={sub.product_id}
              productId={sub.product_id}
              planCode={sub.plan_code}
              size="sm"
            />
          ))}
        </div>
      ) : (
        <div className="popover-no-sub">暂无订阅</div>
      )}
    </div>
  )

  return (
    <>
      <Popover content={popoverContent} position="bottomRight" showArrow>
        <span
          className="lurus-avatar-wrap"
          onClick={() => setCardVisible(true)}
          style={{ cursor: 'pointer' }}
        >
          {account.avatar_url ? (
            <Avatar
              size={size}
              src={account.avatar_url}
            />
          ) : (
            <Avatar
              size={size}
              style={{ background: vipColor, fontSize: size * 0.4 }}
            >
              {initial}
            </Avatar>
          )}
          {vipLevel > 0 && (
            <span
              className="vip-dot"
              style={{ background: vipColor }}
              title={`VIP Lv.${vipLevel}`}
            />
          )}
        </span>
      </Popover>

      {/* Identity card modal */}
      <Modal
        visible={cardVisible}
        onCancel={() => setCardVisible(false)}
        footer={null}
        width={280}
        centered
      >
        <IDCard
          account={account}
          subscriptions={subscriptions}
          onClose={() => setCardVisible(false)}
        />
      </Modal>
    </>
  )
}
