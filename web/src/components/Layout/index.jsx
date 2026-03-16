import React from 'react'
import { Nav, Layout as SemiLayout, Button, Tooltip } from '@douyinfe/semi-ui'
import {
  IconCoinMoney,
  IconCreditCard,
  IconStar,
  IconGift,
  IconExit,
} from '@douyinfe/semi-icons'
import { useNavigate, useLocation, Outlet } from 'react-router-dom'
import { useStore } from '../../store'
import LurusAvatar from '../LurusAvatar'
import { logout } from '../../auth'

const { Header, Sider, Content } = SemiLayout

const NAV_ITEMS = [
  { itemKey: '/wallet',        text: '钱包',   icon: <IconCoinMoney /> },
  { itemKey: '/topup',         text: '充值',   icon: <IconCreditCard /> },
  { itemKey: '/subscriptions', text: '订阅',   icon: <IconStar /> },
  { itemKey: '/redeem',        text: '兑换码', icon: <IconGift /> },
]

function handleLogout() {
  logout()
}

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const { account, subscriptions } = useStore()

  return (
    <SemiLayout style={{ height: '100vh' }}>
      <Sider style={{ background: '#fff', borderRight: '1px solid #f0f0f0', display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: '20px 16px 12px', display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontWeight: 700, fontSize: 18, color: '#1677ff' }}>Lurus</span>
        </div>
        <Nav
          selectedKeys={[location.pathname]}
          items={NAV_ITEMS}
          onSelect={({ itemKey }) => navigate(itemKey)}
          style={{ borderRight: 'none', flex: 1 }}
        />
        <div style={{ padding: '12px 16px', borderTop: '1px solid #f0f0f0' }}>
          <Tooltip content="退出登录" position="right">
            <Button
              icon={<IconExit />}
              type="tertiary"
              theme="borderless"
              onClick={handleLogout}
              style={{ width: '100%', justifyContent: 'flex-start', color: '#8c8c8c' }}
            >
              退出登录
            </Button>
          </Tooltip>
        </div>
      </Sider>
      <SemiLayout>
        <Header
          style={{
            background: '#fff',
            borderBottom: '1px solid #f0f0f0',
            padding: '0 24px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'flex-end',
            height: 56,
          }}
        >
          <LurusAvatar account={account} subscriptions={subscriptions} size={36} />
        </Header>
        <Content style={{ padding: 24, overflowY: 'auto' }}>
          <Outlet />
        </Content>
      </SemiLayout>
    </SemiLayout>
  )
}
