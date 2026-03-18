package sender

import (
	"bytes"
	"html/template"
	"strings"
)

// BaseLayout is the HTML email base layout with brand header and footer.
// Individual email templates are injected into the {{.Content}} block.
const baseLayoutHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  body { margin: 0; padding: 0; background-color: #f5f5f5; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; }
  .container { max-width: 600px; margin: 0 auto; background: #ffffff; }
  .header { background: #1a1a2e; padding: 24px 32px; text-align: center; }
  .header h1 { color: #ffffff; margin: 0; font-size: 24px; font-weight: 600; letter-spacing: 2px; }
  .body { padding: 32px; color: #333333; line-height: 1.6; }
  .body h2 { color: #1a1a2e; margin-top: 0; }
  .cta { display: inline-block; background: #4f46e5; color: #ffffff; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 500; margin: 16px 0; }
  .footer { background: #f9fafb; padding: 24px 32px; text-align: center; font-size: 12px; color: #9ca3af; border-top: 1px solid #e5e7eb; }
  .footer a { color: #6b7280; text-decoration: underline; }
</style>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>LURUS</h1>
  </div>
  <div class="body">
    {{.Content}}
  </div>
  <div class="footer">
    <p>&copy; 2026 Lurus. All rights reserved.</p>
    <p><a href="https://identity.lurus.cn/notifications/preferences">Manage notification preferences</a> | <a href="https://identity.lurus.cn/unsubscribe">Unsubscribe</a></p>
  </div>
</div>
</body>
</html>`

var baseTemplate = template.Must(template.New("base").Parse(baseLayoutHTML))

// RenderHTMLEmail wraps content HTML in the brand base layout.
func RenderHTMLEmail(contentHTML string) (string, error) {
	var buf bytes.Buffer
	if err := baseTemplate.Execute(&buf, map[string]template.HTML{
		"Content": template.HTML(contentHTML),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Pre-defined HTML content templates for common notification types.
var htmlContentTemplates = map[string]string{
	"welcome": `<h2>Welcome to Lurus!</h2>
<p>Your account has been created successfully. We're excited to have you on board.</p>
<p>Get started by exploring our platform:</p>
<a href="https://www.lurus.cn" class="cta">Get Started</a>`,

	"subscription_activated": `<h2>Subscription Activated</h2>
<p>Your <strong>{{plan_code}}</strong> plan is now active.</p>
<p>Your subscription is valid until <strong>{{expires_at}}</strong>.</p>
<p>Enjoy all the premium features!</p>`,

	"subscription_expired": `<h2>Subscription Expired</h2>
<p>Your subscription has expired. Renew now to continue accessing premium features.</p>
<a href="https://identity.lurus.cn/pricing" class="cta">Renew Subscription</a>`,

	"topup": `<h2>Top-up Successful</h2>
<p><strong>{{credits_added}}</strong> credits have been added to your wallet.</p>
<p>Your updated balance is ready for use.</p>`,

	"quota_warning": `<h2>API Quota Warning</h2>
<p>Your API usage has reached <strong>{{percent}}%</strong> of your monthly quota.</p>
<p>You have <strong>{{remaining}}</strong> tokens remaining.</p>
<a href="https://identity.lurus.cn/pricing" class="cta">Upgrade Plan</a>`,

	"quota_exhausted": `<h2>API Quota Exhausted</h2>
<p style="color: #dc2626;">Your API quota has been fully consumed. Service is suspended.</p>
<p>Please upgrade your plan or top up credits to resume access.</p>
<a href="https://identity.lurus.cn/pricing" class="cta">Upgrade Now</a>`,

	"weekly_digest": `<h2>Your Weekly Summary</h2>
<p>Here's a summary of your activity this past week:</p>
{{digest_content}}
<a href="https://www.lurus.cn/dashboard" class="cta">View Dashboard</a>`,
}

// GetHTMLContent returns a rendered HTML email for a known template name,
// with variable substitution applied. Returns empty string if template not found.
func GetHTMLContent(templateName string, vars map[string]string) string {
	content, ok := htmlContentTemplates[templateName]
	if !ok {
		return ""
	}
	// HTML-escape variables to prevent XSS, then substitute into template.
	for k, v := range vars {
		escaped := template.HTMLEscapeString(v)
		content = strings.ReplaceAll(content, "{{"+k+"}}", escaped)
	}
	rendered, err := RenderHTMLEmail(content)
	if err != nil {
		return ""
	}
	return rendered
}
