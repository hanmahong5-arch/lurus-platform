import React from 'react'
import { Link } from 'react-router-dom'

// 用户协议（《Lurus 用户服务协议》）
//
// 内容参考国内同类 SaaS 公司（智谱、MiniMax、DeepSeek、阿里云）的通用模板
// 改写而成。法务终稿前请由律师 review 一遍 — 这里是工程兜底版本，避免上线
// 时缺位。
//
// 修改建议：
//   - 公司主体名称在 LEGAL_ENTITY 常量集中维护，方便后续替换
//   - 章节编号刻意保守（参考国内消费级 SaaS 主流写法）

const LEGAL_ENTITY = '上海路涂科技有限公司'  // TODO: 法人主体确认后改这里
const SERVICE_NAME = 'Lurus 平台'
const CONTACT_EMAIL = 'legal@lurus.cn'
const UPDATED_AT = '2026-04-29'

export default function TosPage() {
  return (
    <div style={pageStyle}>
      <div style={containerStyle}>
        <Link to="/login" style={backLinkStyle}>← 返回登录</Link>
        <h1 style={titleStyle}>{SERVICE_NAME} 用户服务协议</h1>
        <p style={metaStyle}>更新时间：{UPDATED_AT}　|　发布方：{LEGAL_ENTITY}</p>

        <Section title="一、协议接受">
          <p>
            欢迎使用 {SERVICE_NAME}（以下简称"本服务"）。本协议由您与{LEGAL_ENTITY}（以下简称"我们"）共同缔结，
            对双方均具有法律约束力。当您勾选"我已阅读并同意"或以其他方式开始使用本服务时，
            即视为您已充分阅读、理解并接受本协议全部条款。
          </p>
          <p>
            如果您不同意本协议任何条款，请立即停止使用本服务。
          </p>
        </Section>

        <Section title="二、账号注册与使用">
          <p>2.1 您可以通过手机号、邮箱或第三方授权登录方式注册并使用本服务。</p>
          <p>2.2 您应当提供真实、准确、完整的注册信息，并对账号下的所有行为承担责任。</p>
          <p>2.3 您不得将账号、密码出借、转让或与他人共享。如发现账号被盗用，应立即通知我们。</p>
          <p>2.4 我们可能在必要时（如涉嫌违法、违反本协议）暂停或终止您的账号。</p>
        </Section>

        <Section title="三、服务内容">
          <p>
            本服务提供企业 AI 基础设施、统一身份认证、计费与订阅、应用接入等能力。
            具体功能以页面展示为准，我们可能根据业务需要随时调整、增加或减少功能模块。
          </p>
        </Section>

        <Section title="四、付费服务">
          <p>4.1 本服务部分功能为付费功能，您应当按页面展示的价格、计费规则向我们支付费用。</p>
          <p>4.2 您可以通过支付宝、微信支付、信用卡等渠道完成支付。支付成功后，对应权益将立刻或按约定时间生效。</p>
          <p>4.3 除法律法规另有规定外，已支付费用一般不予退还。退款的条件和流程以"退款政策"文档为准。</p>
        </Section>

        <Section title="五、用户行为规范">
          <p>您承诺，使用本服务期间不会从事以下行为：</p>
          <ul style={listStyle}>
            <li>违反国家法律法规、社会公共秩序与善良风俗的行为；</li>
            <li>制作、上传、传播违法、有害、骚扰、侵权内容；</li>
            <li>对本服务进行反向工程、破解、未经授权的访问；</li>
            <li>使用爬虫、自动脚本等手段对本服务造成异常负载；</li>
            <li>利用本服务实施欺诈、洗钱、逃税等违法违规活动。</li>
          </ul>
        </Section>

        <Section title="六、知识产权">
          <p>
            本服务的所有权、知识产权（包括但不限于商标、文档、代码、设计、API 接口）归我们所有。
            您可以在本协议许可范围内使用本服务，但不得未经授权进行复制、转售、再分发。
          </p>
          <p>
            您通过本服务上传或生成的内容，其权利归属您本人；您仅授权我们在提供服务的必要范围内使用。
          </p>
        </Section>

        <Section title="七、免责声明">
          <p>
            7.1 我们将尽商业上合理努力保障服务的稳定性和准确性，但不保证服务永远无中断、无错误。
          </p>
          <p>
            7.2 因不可抗力（如自然灾害、网络攻击、政府行为）、第三方原因（如运营商、上游服务商）
            导致的服务中断或数据损失，我们不承担责任。
          </p>
          <p>
            7.3 在法律允许的最大范围内，我们对您因使用本服务造成的间接损失、利润损失等不承担责任。
          </p>
        </Section>

        <Section title="八、协议变更">
          <p>
            我们可能根据业务需要修订本协议。修订后的协议将在本服务页面发布并自发布之日起生效。
            您继续使用本服务即视为接受修订后的协议；如不接受，应停止使用并联系我们注销账号。
          </p>
        </Section>

        <Section title="九、法律适用与争议解决">
          <p>
            本协议适用中华人民共和国法律。双方因本协议产生的争议，应优先协商解决；协商不成的，
            提交{LEGAL_ENTITY}所在地有管辖权的人民法院诉讼解决。
          </p>
        </Section>

        <Section title="十、联系我们">
          <p>
            如您对本协议有任何疑问、意见或建议，请通过{' '}
            <a href={`mailto:${CONTACT_EMAIL}`} style={linkStyle}>{CONTACT_EMAIL}</a>{' '}
            与我们联系。
          </p>
        </Section>

        <p style={footerNoteStyle}>
          —— 本协议自您同意之时起对您生效 ——
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
  marginBottom: 36,
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
