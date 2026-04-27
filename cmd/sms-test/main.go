// Command sms-test is a one-shot CLI tool for verifying the SMS provider
// configuration. It sends a test OTP SMS to a specified phone number and
// prints the provider response.
//
// Usage:
//
//	export SMS_PROVIDER=aliyun
//	export SMS_ALIYUN_ACCESS_KEY_ID=...
//	export SMS_ALIYUN_ACCESS_KEY_SECRET=...
//	export SMS_ALIYUN_SIGN_NAME=...
//	export SMS_ALIYUN_TEMPLATE_CODE_VERIFY=...
//	go run ./cmd/sms-test -phone +8613800138000 -code 123456
//
// NOTE: Real SMS will be delivered to the target number and billed by
// Aliyun. Use a phone you own for testing. Set -dry-run to skip the
// actual API call and only validate configuration.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	appsms "github.com/hanmahong5-arch/lurus-platform/internal/app/sms"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/sms"
)

func main() {
	phone := flag.String("phone", "", "E.164 phone number to send the test SMS to (e.g. +8613800138000)")
	code := flag.String("code", "123456", "OTP code to include in the message")
	dryRun := flag.Bool("dry-run", false, "Validate config only, do not send SMS")
	flag.Parse()

	if *phone == "" {
		fmt.Fprintln(os.Stderr, "error: -phone is required")
		flag.Usage()
		os.Exit(1)
	}

	cfg := sms.LoadFromEnv()

	log.Printf("SMS provider: %q", cfg.Provider)
	log.Printf("sign_name: %q", cfg.AliyunSignName)
	log.Printf("template_verify: %q", cfg.AliyunTemplateVerify)

	if *dryRun {
		log.Println("dry-run mode: configuration validated, no SMS sent")
		return
	}

	sender, err := sms.NewFromConfig(cfg)
	if err != nil {
		log.Fatalf("init SMS sender: %v", err)
	}

	uc := appsms.NewSMSRelayUsecase(
		sender,
		cfg.AliyunSignName,
		cfg.AliyunTemplateVerify,
		cfg.AliyunTemplateReset,
		3,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Printf("sending OTP %q to %s ...", *code, *phone)
	if err := uc.SendOTP(ctx, *phone, *code); err != nil {
		log.Fatalf("send failed: %v", err)
	}
	log.Println("SMS sent successfully")
}
