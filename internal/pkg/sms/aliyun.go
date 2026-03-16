package sms

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/dysmsapi"
)

// AliyunSender sends SMS via Aliyun Dysms API.
type AliyunSender struct {
	client   *dysmsapi.Client
	signName string
}

// NewAliyunSender creates an AliyunSender from config.
func NewAliyunSender(cfg SMSConfig) *AliyunSender {
	client, _ := dysmsapi.NewClientWithAccessKey("cn-hangzhou", cfg.AliyunAccessKeyID, cfg.AliyunAccessKeySecret)
	return &AliyunSender{
		client:   client,
		signName: cfg.AliyunSignName,
	}
}

func (s *AliyunSender) Send(_ context.Context, phone, templateCode string, params map[string]string) error {
	req := dysmsapi.CreateSendSmsRequest()
	req.Scheme = "https"
	req.PhoneNumbers = phone
	req.SignName = s.signName
	req.TemplateCode = templateCode

	if len(params) > 0 {
		paramJSON, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("aliyun sms: marshal params: %w", err)
		}
		req.TemplateParam = string(paramJSON)
	}

	resp, err := s.client.SendSms(req)
	if err != nil {
		return fmt.Errorf("aliyun sms: send failed: %w", err)
	}
	if resp.Code != "OK" {
		return fmt.Errorf("aliyun sms: send error code=%s message=%s", resp.Code, resp.Message)
	}
	return nil
}
