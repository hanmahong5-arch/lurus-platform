import React, { useEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import WalletPage from './pages/Wallet'
import TopupPage from './pages/Topup'
import SubscriptionsPage from './pages/Subscriptions'
import RedeemPage from './pages/Redeem'
import AdminPage from './pages/Admin'
import CallbackPage from './pages/Callback'
import LoginPage from './pages/Login'
import ForgotPasswordPage from './pages/ForgotPassword'
import ZLoginPage from './pages/ZLogin'
import HubPage from './pages/Hub'
import TosPage from './pages/Legal/Tos'
import PrivacyPage from './pages/Legal/Privacy'
import { useStore } from './store'
import { isLoggedIn } from './auth'

function RequireAuth({ children }) {
  if (!isLoggedIn()) {
    const path = window.location.pathname
    if (path && path !== '/') {
      sessionStorage.setItem('login_return', path)
    }
    window.location.href = '/login'
    return null
  }
  return children
}

export default function App() {
  const init = useStore((s) => s.init)

  useEffect(() => {
    // Skip init on auth pages — /callback hasn't stored the token yet,
    // and /login + /zlogin don't need API data.
    const path = window.location.pathname
    if (['/callback', '/login', '/register', '/forgot-password', '/zlogin', '/legal/tos', '/legal/privacy'].includes(path)) return
    if (isLoggedIn()) init()
  }, [])

  return (
    <BrowserRouter>
      <Routes>
        {/* Auth pages — outside Layout, no auth required */}
        <Route path="/login" element={<LoginPage />} />
        {/* Register kept as alias to /login — the new combined "登录/注册"
            button auto-creates an account on first SMS verification, so
            there's no separate registration screen any more. */}
        <Route path="/register" element={<Navigate to="/login" replace />} />
        <Route path="/forgot-password" element={<ForgotPasswordPage />} />
        <Route path="/callback" element={<CallbackPage />} />
        {/* Custom Zitadel OIDC login UI — no auth, no layout wrapper */}
        <Route path="/zlogin" element={<ZLoginPage />} />
        {/* Legal pages — public, no auth required */}
        <Route path="/legal/tos" element={<TosPage />} />
        <Route path="/legal/privacy" element={<PrivacyPage />} />

        <Route path="/" element={<RequireAuth><Layout /></RequireAuth>}>
          <Route index element={<Navigate to="/hub" replace />} />
          <Route path="hub" element={<HubPage />} />
          <Route path="wallet" element={<WalletPage />} />
          <Route path="topup" element={<TopupPage />} />
          <Route path="subscriptions" element={<SubscriptionsPage />} />
          <Route path="redeem" element={<RedeemPage />} />
          <Route path="admin/*" element={<AdminPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
