import React from 'react'
import { Link } from 'react-router-dom'

// Footer — 国内合规与用户信任元素：
//
//   1. ICP 备案号  — 工信部要求所有 .cn 域名站点必须显示
//   2. 公安备案号  — 北京/上海等地 ISP 经营性站点强制要求
//   3. 客服联系   — 国内用户更习惯邮箱/电话明示，而非"在线客服"按钮
//   4. 法律入口   — 用户协议 + 隐私政策直链
//
// 备案号未公布时显示 "ICP 备案审核中" — 不阻塞产品上线，运维拿到号后改一行字。
//
// 设计目标：单文件、零外部依赖（不引 Semi UI 让法律页等公开页面也能直接用），
// 在登录页 / Hub / 法律页 三处共用。

// 占位符 — 运维拿到正式号后改这里。也可通过 build-time env 注入；
// 现在保持 inline 让 audit 时可以一眼看到当前状态。
const ICP_NUMBER = '京 ICP 备案审核中'        // TODO: 上线前替换为真实备案号
const PUBLIC_SECURITY_NUMBER = ''             // TODO: 公安备案号（如适用）
const SUPPORT_EMAIL = 'support@lurus.cn'
const COPYRIGHT_HOLDER = '上海路涂科技有限公司' // TODO: 法人主体确认后改这里

// year — 自动更新，避免每年人工改
const CURRENT_YEAR = new Date().getFullYear()

// `compact` 让登录页用简化版（避免占据小屏太多空间）；
// `dark` 适配暗色背景（登录页 / 法律页背景色不同）
export default function Footer({ compact = false, dark = false }) {
  const textColor = dark ? '#94a3b8' : '#9ca3af'
  const linkColor = dark ? '#cbd5e1' : '#6b7280'

  return (
    <footer style={{
      ...containerStyle,
      color: textColor,
      ...(compact ? compactStyle : {}),
    }}>
      <div style={legalLinksStyle}>
        <Link to="/legal/tos" style={{ ...linkStyle, color: linkColor }}>用户协议</Link>
        <span style={separatorStyle} aria-hidden>·</span>
        <Link to="/legal/privacy" style={{ ...linkStyle, color: linkColor }}>隐私政策</Link>
        <span style={separatorStyle} aria-hidden>·</span>
        <a href={`mailto:${SUPPORT_EMAIL}`} style={{ ...linkStyle, color: linkColor }}>
          联系客服
        </a>
      </div>

      <div style={legalTextStyle}>
        © {CURRENT_YEAR} {COPYRIGHT_HOLDER}．保留所有权利．
      </div>

      <div style={legalTextStyle}>
        {/* 备案号链接到工信部查询页 — 用户可一键核验，国内 SEO 信任分加成 */}
        <a
          href="https://beian.miit.gov.cn/"
          target="_blank"
          rel="noopener noreferrer"
          style={{ ...linkStyle, color: linkColor }}
        >
          {ICP_NUMBER}
        </a>
        {PUBLIC_SECURITY_NUMBER && (
          <>
            <span style={separatorStyle} aria-hidden>·</span>
            <a
              href={`https://www.beian.gov.cn/portal/registerSystemInfo?recordcode=${encodeURIComponent(PUBLIC_SECURITY_NUMBER.replace(/\D/g, ''))}`}
              target="_blank"
              rel="noopener noreferrer"
              style={{ ...linkStyle, color: linkColor }}
            >
              {PUBLIC_SECURITY_NUMBER}
            </a>
          </>
        )}
      </div>
    </footer>
  )
}

// ── inline styles（避免引入新 CSS 文件，保持组件可移植）─────────────────
const containerStyle = {
  width: '100%',
  textAlign: 'center',
  padding: '24px 16px 20px',
  fontSize: 12,
  lineHeight: 1.8,
  fontFamily: '-apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif',
}

const compactStyle = {
  padding: '16px 16px 12px',
  fontSize: 11,
  lineHeight: 1.6,
}

const legalLinksStyle = {
  marginBottom: 6,
}

const legalTextStyle = {
  display: 'block',
}

const linkStyle = {
  textDecoration: 'none',
  margin: '0 4px',
}

const separatorStyle = {
  margin: '0 4px',
  opacity: 0.5,
}
