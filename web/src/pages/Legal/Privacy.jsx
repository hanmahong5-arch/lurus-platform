import React from 'react'
import { Link } from 'react-router-dom'

// 隐私政策（《Lurus 隐私政策》）
//
// 内容参考国内主流 SaaS 公司模板改写。结构覆盖《个人信息保护法》(PIPL)
// 强制要求的最小信息项，外加合理范围内的解释性陈述。
//
// 法务终稿前请由律师 review。这是工程兜底版本。

const LEGAL_ENTITY = '上海路涂科技有限公司'
const SERVICE_NAME = 'Lurus 平台'
const DPO_EMAIL = 'privacy@lurus.cn'
const UPDATED_AT = '2026-04-29'

export default function PrivacyPage() {
  return (
    <div style={pageStyle}>
      <div style={containerStyle}>
        <Link to="/login" style={backLinkStyle}>← 返回登录</Link>
        <h1 style={titleStyle}>{SERVICE_NAME} 隐私政策</h1>
        <p style={metaStyle}>更新时间：{UPDATED_AT}　|　发布方：{LEGAL_ENTITY}</p>

        <p style={leadStyle}>
          {LEGAL_ENTITY}（"我们"）尊重并保护用户的个人信息。
          本政策说明我们收集、使用、存储、共享及保护您个人信息的方式，
          请您在使用 {SERVICE_NAME} 前仔细阅读。
        </p>

        <Section title="一、我们收集的信息">
          <p>1.1 <b>注册信息</b>：手机号、邮箱、密码（哈希存储）、用户昵称等。</p>
          <p>1.2 <b>登录与设备信息</b>：登录时间、IP 地址、设备型号、操作系统、浏览器版本等。</p>
          <p>1.3 <b>使用信息</b>：您调用本服务的 API 记录、消费的算力/额度、订阅状态等。</p>
          <p>1.4 <b>支付信息</b>：通过第三方支付通道（支付宝、微信支付等）完成支付时，我们仅接收支付结果，不存储您的银行卡号、CVV 等敏感数据。</p>
          <p>1.5 <b>第三方账号信息</b>：当您选择使用微信、Apple 等第三方账号登录时，我们仅获取必要的标识信息（openid/unionid）。</p>
        </Section>

        <Section title="二、我们如何使用您的信息">
          <ul style={listStyle}>
            <li>提供本服务的核心功能（账号注册、登录、订阅、计费、客服）；</li>
            <li>账号安全保护（异常登录检测、风险拦截）；</li>
            <li>统计分析与产品改进（已脱敏、聚合形式）；</li>
            <li>履行法定义务（如配合监管机构调查）；</li>
            <li>仅在征得您同意的前提下，向您发送营销信息。</li>
          </ul>
        </Section>

        <Section title="三、信息存储与保护">
          <p>3.1 您的个人信息存储于中国大陆境内服务器，存储期限不超过实现服务目的所必需的时长。</p>
          <p>3.2 我们采取行业标准的安全措施保护您的个人信息：</p>
          <ul style={listStyle}>
            <li>密码：仅以 SHA-256 / bcrypt 哈希形式存储，不存储明文；</li>
            <li>传输：全程 TLS 1.2+ 加密；</li>
            <li>访问控制：内部实行最小权限原则，关键操作要求多因素认证；</li>
            <li>审计：所有特权操作（如批量退款、删除账号）均产生不可篡改的审计日志。</li>
          </ul>
          <p>3.3 即便如此，互联网环境不存在 100% 安全。如发生数据泄露，我们将依法在 72 小时内通知您并向监管机构报告。</p>
        </Section>

        <Section title="四、信息共享与对外披露">
          <p>除以下情形外，我们不会将您的个人信息向任何第三方共享、转让或披露：</p>
          <ul style={listStyle}>
            <li>事先获得您的明确同意；</li>
            <li>与我们的服务提供商（如阿里云短信、Stripe 支付）共享必要信息以完成服务交付；</li>
            <li>法律法规要求或司法/行政机关依职权要求。</li>
          </ul>
          <p>所有获得您信息的第三方均与我们签订严格的保密协议，并仅在服务目的范围内使用。</p>
        </Section>

        <Section title="五、您的权利">
          <p>根据《个人信息保护法》及相关法规，您对自己的个人信息享有以下权利：</p>
          <ul style={listStyle}>
            <li><b>查阅、复制权</b>：您可在账户中心查看个人信息；</li>
            <li><b>更正权</b>：您可随时修改昵称、绑定邮箱等信息；</li>
            <li><b>删除权</b>：您可申请注销账号，注销后我们将删除或匿名化您的个人信息；</li>
            <li><b>撤回同意权</b>：您可随时撤回此前授予我们的同意（如关闭营销推送）；</li>
            <li><b>投诉权</b>：如对我们的处理方式有异议，可向{' '}
              <a href={`mailto:${DPO_EMAIL}`} style={linkStyle}>{DPO_EMAIL}</a>{' '}
              发送邮件投诉。
            </li>
          </ul>
        </Section>

        <Section title="六、Cookie 与同类技术">
          <p>
            我们在必要时使用 Cookie 维持登录状态、记忆您的偏好。您可通过浏览器设置禁用 Cookie，
            但这可能导致部分功能不可用。
          </p>
        </Section>

        <Section title="七、未成年人保护">
          <p>
            本服务不针对未满 14 周岁的儿童提供。如您未满 14 周岁，请勿使用本服务；
            如我们发现误收集了儿童个人信息，将立即删除。
          </p>
        </Section>

        <Section title="八、政策变更">
          <p>
            我们可能根据业务发展或法律法规调整本政策。
            重大变更（如个人信息处理目的扩大、对外共享范围扩大）将通过站内公告、邮件等方式提前通知您。
          </p>
        </Section>

        <Section title="九、联系我们">
          <p>
            个人信息保护负责人邮箱：
            <a href={`mailto:${DPO_EMAIL}`} style={linkStyle}>{DPO_EMAIL}</a>
          </p>
          <p>
            如您不满意我们的回复，您可向所在地的网信、公安、市场监管部门投诉举报。
          </p>
        </Section>

        <p style={footerNoteStyle}>
          —— 本政策自您同意之时起对您生效 ——
        </p>
      </div>
    </div>
  )
}

function Section({ title, children }) {
  return (
    <section style={{ marginBottom: 24 }}>
      <h2 style={sectionTitleStyle}>{title}</h2>
      <div style={{ color: '#374151', lineHeight: 1.75, fontSize: 14 }}>
        {children}
      </div>
    </section>
  )
}

const pageStyle = {
  minHeight: '100vh',
  background: '#f9fafb',
  padding: '40px 20px',
  fontFamily: '-apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif',
}

const containerStyle = {
  maxWidth: 760,
  margin: '0 auto',
  background: '#fff',
  borderRadius: 12,
  padding: '40px 48px',
  boxShadow: '0 2px 8px rgba(0,0,0,0.04)',
}

const backLinkStyle = {
  color: '#6b7280',
  textDecoration: 'none',
  fontSize: 13,
  marginBottom: 24,
  display: 'inline-block',
}

const titleStyle = {
  fontSize: 24,
  fontWeight: 700,
  color: '#111827',
  marginTop: 8,
  marginBottom: 8,
}

const metaStyle = {
  color: '#9ca3af',
  fontSize: 12,
  marginBottom: 24,
}

const leadStyle = {
  color: '#374151',
  lineHeight: 1.75,
  fontSize: 14,
  marginBottom: 28,
  paddingLeft: 12,
  borderLeft: '3px solid #1677ff',
}

const sectionTitleStyle = {
  fontSize: 16,
  fontWeight: 600,
  color: '#111827',
  marginTop: 20,
  marginBottom: 8,
}

const listStyle = {
  paddingLeft: 22,
  margin: '8px 0',
}

const linkStyle = {
  color: '#1677ff',
  textDecoration: 'none',
}

const footerNoteStyle = {
  textAlign: 'center',
  color: '#9ca3af',
  fontSize: 12,
  marginTop: 40,
  paddingTop: 24,
  borderTop: '1px solid #e5e7eb',
}
