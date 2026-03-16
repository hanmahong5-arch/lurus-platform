package sms

import (
	"context"
	"fmt"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tencentSMS "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

// TencentSender sends SMS via Tencent Cloud SMS API.
type TencentSender struct {
	client   *tencentSMS.Client
	appID    string
	signName string
}

// NewTencentSender creates a TencentSender from config.
func NewTencentSender(cfg SMSConfig) *TencentSender {
	credential := common.NewCredential(cfg.TencentSecretID, cfg.TencentSecretKey)
	cpf := profile.NewClientProfile()
	client, _ := tencentSMS.NewClient(credential, "ap-guangzhou", cpf)
	return &TencentSender{
		client:   client,
		appID:    cfg.TencentAppID,
		signName: cfg.TencentSignName,
	}
}

func (s *TencentSender) Send(ctx context.Context, phone, templateID string, params map[string]string) error {
	req := tencentSMS.NewSendSmsRequest()
	req.SmsSdkAppId = common.StringPtr(s.appID)
	req.SignName = common.StringPtr(s.signName)
	req.TemplateId = common.StringPtr(templateID)

	// Tencent SMS requires +86 prefix for China mainland numbers.
	if len(phone) == 11 {
		phone = "+86" + phone
	}
	req.PhoneNumberSet = common.StringPtrs([]string{phone})

	// Convert params map to ordered template param values.
	// Tencent SMS uses positional params {1}, {2}, etc.
	var vals []string
	if code, ok := params["code"]; ok {
		vals = append(vals, code)
	}
	if ttl, ok := params["ttl"]; ok {
		vals = append(vals, ttl)
	}
	req.TemplateParamSet = common.StringPtrs(vals)

	resp, err := s.client.SendSms(req)
	if err != nil {
		return fmt.Errorf("tencent sms: send failed: %w", err)
	}

	// Check per-number status.
	if len(resp.Response.SendStatusSet) > 0 {
		status := resp.Response.SendStatusSet[0]
		if status.Code != nil && *status.Code != "Ok" {
			return fmt.Errorf("tencent sms: send error code=%s message=%s", *status.Code, *status.Message)
		}
	}
	return nil
}
