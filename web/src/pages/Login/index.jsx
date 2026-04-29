import React, { useState, useEffect } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { storeLurusToken, isLoggedIn } from '../../auth'
import { useStore } from '../../store'

// Lurus 统一登录页 — MiniMax-风格双 Tab + 统一"登录/注册"按钮
//
// 设计原则（来自用户反馈"不要搞复杂"）：
//   1. 一个页面解决登录 + 注册，没有"先注册"的多步流程
//   2. 默认手机验证码（中国市场首选，也免去密码强度规则）
//   3. 必须勾用户协议 + 隐私政策才能提交
//   4. 不暴露 Zitadel/OIDC 概念给终端用户
//
// 登录后 token 存 localStorage（沿用现有约定），并在父域 .lurus.cn 写
// session cookie 让所有 *.lurus.cn 子域共享（drop-in SDK 的基础）。
//
// 后端端点（已存在 + TODO）：
//   POST /api/v1/auth/login                — 账号密码登录（已实现）
//   POST /api/v1/auth/send-sms             — 发短信验证码（已实现）
//   POST /api/v1/auth/login-or-register    — 验证码登录/注册一体化（**TODO 后端实现**）
//
// 在 login-or-register 端点上线前，"密码 Tab" 完全可用；"手机验证码 Tab"
// 走已有的 send-sms + 一个临时的客户端组合 (login → 失败则 register) 兜底。

export default function LoginPage() {
  const navigate = useNavigate()
  const init = useStore((s) => s.init)

  const [activeTab, setActiveTab] = useState('sms') // 'sms' | 'password'
  const [submitting, setSubmitting] = useState(false)
  const [errMsg, setErrMsg] = useState('')

  // Password tab state
  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')

  // SMS tab state
  const [phone, setPhone] = useState('')
  const [smsCode, setSmsCode] = useState('')
  const [smsCountdown, setSmsCountdown] = useState(0)
  const [sendingCode, setSendingCode] = useState(false)

  // Shared
  const [agreed, setAgreed] = useState(false)

  // Already logged in — redirect immediately.
  useEffect(() => {
    if (isLoggedIn()) {
      const returnTo = sessionStorage.getItem('login_return') || '/hub'
      sessionStorage.removeItem('login_return')
      window.location.href = returnTo
    }
  }, [])

  // Countdown for SMS resend cooldown.
  useEffect(() => {
    if (smsCountdown <= 0) return
    const t = setTimeout(() => setSmsCountdown(smsCountdown - 1), 1000)
    return () => clearTimeout(t)
  }, [smsCountdown])

  function isValidPhoneCN(p) {
    return /^1[3-9]\d{9}$/.test(p)
  }

  async function handleSendCode() {
    setErrMsg('')
    if (!isValidPhoneCN(phone)) {
      setErrMsg('请输入正确的中国大陆手机号')
      return
    }
    setSendingCode(true)
    try {
      const res = await fetch('/api/v1/auth/send-sms', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone: '+86' + phone, channel: 'login' }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok && res.status !== 200) {
        throw new Error(data.error || '发送失败')
      }
      setSmsCountdown(60)
    } catch (err) {
      setErrMsg(err.message || '发送失败，请稍后重试')
    } finally {
      setSendingCode(false)
    }
  }

  async function postLoginSuccess(token) {
    storeLurusToken(token)
    try { await init() } catch { /* swallow init errors — auth still succeeded */ }
    const returnTo = sessionStorage.getItem('login_return') || '/hub'
    sessionStorage.removeItem('login_return')
    navigate(returnTo, { replace: true })
  }

  async function handlePasswordSubmit() {
    setErrMsg('')
    if (!agreed) { setErrMsg('请先勾选并同意用户协议、隐私政策'); return }
    if (!identifier || !password) { setErrMsg('请输入账号和密码'); return }
    setSubmitting(true)
    try {
      const res = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ identifier, password }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || '登录失败')
      await postLoginSuccess(data.token)
    } catch (err) {
      setErrMsg(err.message || '登录失败')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleSmsSubmit() {
    setErrMsg('')
    if (!agreed) { setErrMsg('请先勾选并同意用户协议、隐私政策'); return }
    if (!isValidPhoneCN(phone)) { setErrMsg('请输入正确的中国大陆手机号'); return }
    if (!/^\d{4,8}$/.test(smsCode)) { setErrMsg('请输入收到的验证码'); return }
    setSubmitting(true)
    try {
      // Try the unified login-or-register endpoint first. Fall back to
      // explicit register on 404 so the page still works during the
      // transition window before the unified backend lands.
      let res = await fetch('/api/v1/auth/login-or-register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone: '+86' + phone, sms_code: smsCode }),
      })
      if (res.status === 404) {
        // Endpoint not yet implemented — temporary client-side fallback:
        // register endpoint accepts phone + code + auto-fills account.
        res = await fetch('/api/v1/auth/register', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ phone: '+86' + phone, sms_code: smsCode, auto_login: true }),
        })
      }
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || '登录失败')
      await postLoginSuccess(data.token)
    } catch (err) {
      setErrMsg(err.message || '登录失败，请重试')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="min-h-screen w-full flex items-center justify-center bg-gradient-to-br from-slate-50 to-slate-100" style={fallbackBg}>
      <div style={cardStyle}>
        {/* Logo + 标题 */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 36 }}>
          <div style={logoMark}>L</div>
          <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: 1, color: '#1f2937' }}>
            LURUS<span style={{ color: '#9ca3af', fontWeight: 400, marginLeft: 10 }}>账户登录</span>
          </div>
        </div>

        {/* Tab switcher */}
        <div style={{ display: 'flex', gap: 28, marginBottom: 24, borderBottom: 'none' }}>
          <TabButton active={activeTab === 'password'} onClick={() => { setActiveTab('password'); setErrMsg('') }}>
            密码登录
          </TabButton>
          <TabButton active={activeTab === 'sms'} onClick={() => { setActiveTab('sms'); setErrMsg('') }}>
            手机验证码登录
          </TabButton>
        </div>

        {/* Form */}
        {activeTab === 'password' ? (
          <div>
            <input
              style={inputStyle}
              type="text"
              placeholder="请输入手机号/邮箱"
              value={identifier}
              onChange={(e) => setIdentifier(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handlePasswordSubmit()}
            />
            <input
              style={{ ...inputStyle, marginTop: 12 }}
              type="password"
              placeholder="请输入登录密码"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handlePasswordSubmit()}
            />
          </div>
        ) : (
          <div>
            <div style={{ ...inputStyle, display: 'flex', alignItems: 'center', padding: 0 }}>
              <span style={{ padding: '0 12px', color: '#6b7280', borderRight: '1px solid #e5e7eb' }}>+86</span>
              <input
                style={{ flex: 1, border: 'none', outline: 'none', padding: '0 14px', height: 46, background: 'transparent' }}
                type="tel"
                inputMode="numeric"
                placeholder="13311188779"
                value={phone}
                onChange={(e) => setPhone(e.target.value.replace(/\D/g, '').slice(0, 11))}
              />
            </div>
            <div style={{ ...inputStyle, marginTop: 12, display: 'flex', alignItems: 'center', padding: 0 }}>
              <input
                style={{ flex: 1, border: 'none', outline: 'none', padding: '0 14px', height: 46, background: 'transparent' }}
                type="text"
                inputMode="numeric"
                placeholder="请输入验证码"
                value={smsCode}
                onChange={(e) => setSmsCode(e.target.value.replace(/\D/g, '').slice(0, 8))}
                onKeyDown={(e) => e.key === 'Enter' && handleSmsSubmit()}
              />
              <button
                style={{
                  ...sendCodeBtn,
                  color: smsCountdown > 0 ? '#9ca3af' : '#1677ff',
                  cursor: (smsCountdown > 0 || sendingCode) ? 'not-allowed' : 'pointer',
                }}
                disabled={smsCountdown > 0 || sendingCode}
                onClick={handleSendCode}
              >
                {sendingCode ? '发送中...' : smsCountdown > 0 ? `${smsCountdown}s 后重发` : '获取验证码'}
              </button>
            </div>
          </div>
        )}

        {/* Error banner */}
        {errMsg && (
          <div style={errBoxStyle}>{errMsg}</div>
        )}

        {/* Agreement */}
        <label style={agreeRowStyle}>
          <input
            type="checkbox"
            checked={agreed}
            onChange={(e) => setAgreed(e.target.checked)}
            style={{ marginRight: 8, width: 16, height: 16, cursor: 'pointer' }}
          />
          <span>
            我已仔细查看并同意该{' '}
            <Link to="/legal/tos" target="_blank" style={linkStyle}>用户协议</Link>
            {' '}与{' '}
            <Link to="/legal/privacy" target="_blank" style={linkStyle}>隐私政策</Link>
          </span>
        </label>

        {/* Submit */}
        <button
          style={{
            ...submitBtnStyle,
            opacity: (submitting || !agreed) ? 0.6 : 1,
            cursor: (submitting || !agreed) ? 'not-allowed' : 'pointer',
          }}
          disabled={submitting || !agreed}
          onClick={activeTab === 'password' ? handlePasswordSubmit : handleSmsSubmit}
        >
          {submitting ? '正在登录...' : '登录 / 注册'}
        </button>

        {/* Footer links */}
        <div style={footerRowStyle}>
          <Link to="/forgot-password" style={footerLinkStyle}>忘记密码</Link>
          <span style={{ flex: 1 }} />
          <a href="#" onClick={(e) => { e.preventDefault(); setActiveTab('sms') }} style={footerLinkStyle}>
            手机号登录
          </a>
        </div>

        {/* Tertiary: QR for power users — kept compact, not a primary flow */}
        <div style={{ marginTop: 28, textAlign: 'center' }}>
          <Link to="/zlogin?qr=1" style={{ ...footerLinkStyle, fontSize: 12 }}>
            使用 Lurus APP 扫码登录
          </Link>
        </div>
      </div>
    </div>
  )
}

// ── Tab button ─────────────────────────────────────────────────────────────
function TabButton({ active, onClick, children }) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        background: 'none',
        border: 'none',
        padding: '8px 0',
        fontSize: 16,
        fontWeight: active ? 600 : 400,
        color: active ? '#1677ff' : '#6b7280',
        cursor: 'pointer',
        borderBottom: active ? '2px solid #1677ff' : '2px solid transparent',
        transition: 'all 0.15s',
      }}
    >
      {children}
    </button>
  )
}

// ── Inline styles (避免引入新 CSS 文件，独立可移植) ─────────────────────────
const fallbackBg = {
  background: 'linear-gradient(135deg, #f8fafc 0%, #eef2ff 100%)',
}

const cardStyle = {
  width: 440,
  background: '#ffffff',
  borderRadius: 16,
  boxShadow: '0 8px 32px rgba(0,0,0,0.08)',
  padding: '40px 48px 36px',
  fontFamily: '-apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif',
}

const logoMark = {
  width: 36,
  height: 36,
  borderRadius: 8,
  background: 'linear-gradient(135deg, #1677ff, #0052cc)',
  color: '#fff',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  fontWeight: 700,
  fontSize: 20,
  fontFamily: 'monospace',
}

const inputStyle = {
  width: '100%',
  height: 46,
  padding: '0 14px',
  border: '1px solid #e5e7eb',
  borderRadius: 8,
  background: '#f9fafb',
  fontSize: 14,
  outline: 'none',
  boxSizing: 'border-box',
}

const sendCodeBtn = {
  background: 'transparent',
  border: 'none',
  padding: '0 16px',
  fontSize: 13,
  fontWeight: 500,
  whiteSpace: 'nowrap',
}

const errBoxStyle = {
  marginTop: 12,
  padding: '8px 12px',
  background: '#fef2f2',
  border: '1px solid #fecaca',
  borderRadius: 6,
  color: '#dc2626',
  fontSize: 13,
}

const agreeRowStyle = {
  display: 'flex',
  alignItems: 'flex-start',
  margin: '16px 0 20px',
  fontSize: 13,
  color: '#6b7280',
  cursor: 'pointer',
  userSelect: 'none',
}

const linkStyle = {
  color: '#1677ff',
  textDecoration: 'none',
}

const submitBtnStyle = {
  width: '100%',
  height: 48,
  background: '#111827',
  color: '#ffffff',
  border: 'none',
  borderRadius: 8,
  fontSize: 16,
  fontWeight: 500,
  letterSpacing: 1,
  transition: 'opacity 0.15s',
}

const footerRowStyle = {
  display: 'flex',
  alignItems: 'center',
  marginTop: 18,
  fontSize: 13,
}

const footerLinkStyle = {
  color: '#6b7280',
  textDecoration: 'none',
}
