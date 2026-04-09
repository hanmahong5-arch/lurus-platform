package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-pay/gopay"
	"github.com/go-pay/gopay/wechat/v3"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// WechatPayProvider implements Provider, Refunder, and NotifyHandler for direct WeChat Pay v3.
type WechatPayProvider struct {
	client    *wechat.ClientV3
	appID     string
	mchID     string
	apiV3Key  string
	notifyURL string
	tradeType TradeType
}

// WechatPayConfig holds configuration for direct WeChat Pay v3 integration.
type WechatPayConfig struct {
	MchID      string // WECHAT_PAY_MCH_ID (merchant ID)
	SerialNo   string // WECHAT_PAY_SERIAL_NO (merchant certificate serial number)
	APIv3Key   string // WECHAT_PAY_API_V3_KEY (for decrypting callbacks)
	PrivateKey string // WECHAT_PAY_PRIVATE_KEY (merchant RSA private key, PEM content)
	AppID      string // WECHAT_PAY_APP_ID (WeChat app ID bound to merchant)
	NotifyURL  string // WECHAT_PAY_NOTIFY_URL (async callback URL)
	IsProd     bool   // WECHAT_PAY_IS_PROD (default true)
}

// NewWechatPayProvider creates a WeChat Pay v3 provider using go-pay.
// Returns (nil, nil) if MchID or APIv3Key is empty (feature disabled).
func NewWechatPayProvider(cfg WechatPayConfig, tradeType TradeType) (*WechatPayProvider, error) {
	if cfg.MchID == "" || cfg.APIv3Key == "" || cfg.PrivateKey == "" {
		return nil, nil
	}

	client, err := wechat.NewClientV3(cfg.MchID, cfg.SerialNo, cfg.APIv3Key, cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("wechat pay new client: %w", err)
	}

	// Auto-verify WeChat platform certificates (recommended).
	if err := client.AutoVerifySign(); err != nil {
		// Non-fatal: log and continue — manual verification still works.
		slog.Warn("wechat pay: auto verify sign setup failed, manual verification required", "err", err)
	}

	return &WechatPayProvider{
		client:    client,
		appID:     cfg.AppID,
		mchID:     cfg.MchID,
		apiV3Key:  cfg.APIv3Key,
		notifyURL: cfg.NotifyURL,
		tradeType: tradeType,
	}, nil
}

// Name returns the provider identifier based on trade type.
func (p *WechatPayProvider) Name() string {
	switch p.tradeType {
	case TradeTypeWAP:
		return "wechat_h5"
	case TradeTypeJSAPI:
		return "wechat_jsapi"
	default:
		return "wechat_native"
	}
}

// CreateCheckout creates a WeChat Pay order and returns the pay URL or prepay info.
func (p *WechatPayProvider) CreateCheckout(ctx context.Context, o *entity.PaymentOrder, _ string) (payURL, externalID string, err error) {
	amountFen := int(o.AmountCNY * 100) // CNY to fen (smallest unit)
	if amountFen < 1 {
		amountFen = 1
	}

	bm := gopay.BodyMap{}
	bm.Set("appid", p.appID)
	bm.Set("description", fmt.Sprintf("Lurus 充值 %.2f CNY", o.AmountCNY))
	bm.Set("out_trade_no", o.OrderNo)
	bm.Set("notify_url", p.notifyURL)
	bm.SetBodyMap("amount", func(bm gopay.BodyMap) {
		bm.Set("total", amountFen)
		bm.Set("currency", "CNY")
	})

	tradeType := p.tradeType
	// Allow per-order override via PaymentMethod field.
	switch o.PaymentMethod {
	case "wechat_h5":
		tradeType = TradeTypeWAP
	case "wechat_jsapi":
		tradeType = TradeTypeJSAPI
	case "wechat_native":
		tradeType = TradeTypeNative
	}

	switch tradeType {
	case TradeTypeWAP:
		bm.SetBodyMap("scene_info", func(bm gopay.BodyMap) {
			bm.Set("payer_client_ip", "127.0.0.1")
			bm.SetBodyMap("h5_info", func(bm gopay.BodyMap) {
				bm.Set("type", "Wap")
			})
		})
		resp, err := p.client.V3TransactionH5(ctx, bm)
		if err != nil {
			return "", "", fmt.Errorf("wechat h5 pay: %w", err)
		}
		if resp.Response.H5Url == "" {
			return "", "", fmt.Errorf("wechat h5 pay: empty h5_url")
		}
		return resp.Response.H5Url, o.OrderNo, nil

	case TradeTypeJSAPI:
		// JSAPI requires payer openid — must be passed via order metadata.
		openid := extractOpenID(o)
		if openid == "" {
			return "", "", fmt.Errorf("wechat jsapi: payer openid is required, set it in order metadata")
		}
		bm.SetBodyMap("payer", func(bm gopay.BodyMap) {
			bm.Set("openid", openid)
		})
		resp, err := p.client.V3TransactionJsapi(ctx, bm)
		if err != nil {
			return "", "", fmt.Errorf("wechat jsapi pay: %w", err)
		}
		if resp.Response.PrepayId == "" {
			return "", "", fmt.Errorf("wechat jsapi pay: empty prepay_id")
		}
		// Return prepay_id — the frontend uses it with wx.requestPayment().
		return resp.Response.PrepayId, o.OrderNo, nil

	default: // TradeTypeNative — QR code
		resp, err := p.client.V3TransactionNative(ctx, bm)
		if err != nil {
			return "", "", fmt.Errorf("wechat native pay: %w", err)
		}
		if resp.Response.CodeUrl == "" {
			return "", "", fmt.Errorf("wechat native pay: empty code_url")
		}
		return resp.Response.CodeUrl, o.OrderNo, nil
	}
}

// Refund initiates a refund at WeChat Pay.
func (p *WechatPayProvider) Refund(ctx context.Context, refundNo, orderNo string, refundAmount, totalAmount float64) error {
	refundFen := int(refundAmount * 100)
	totalFen := int(totalAmount * 100)
	if refundFen < 1 {
		refundFen = 1
	}
	if totalFen < refundFen {
		totalFen = refundFen
	}

	bm := gopay.BodyMap{}
	bm.Set("out_trade_no", orderNo)
	bm.Set("out_refund_no", refundNo)
	bm.SetBodyMap("amount", func(bm gopay.BodyMap) {
		bm.Set("refund", refundFen)
		bm.Set("total", totalFen)
		bm.Set("currency", "CNY")
	})

	resp, err := p.client.V3Refund(ctx, bm)
	if err != nil {
		return fmt.Errorf("wechat refund: %w", err)
	}
	if resp.Response.Status == "ABNORMAL" || resp.Response.Status == "CLOSED" {
		return fmt.Errorf("wechat refund: status=%s", resp.Response.Status)
	}
	slog.Info("wechat/refund", "refund_no", refundNo, "order_no", orderNo, "amount_fen", refundFen, "status", resp.Response.Status)
	return nil
}

// HandleNotify parses and verifies a WeChat Pay v3 async notification.
func (p *WechatPayProvider) HandleNotify(req *http.Request) (orderNo string, ok bool, err error) {
	notifyReq, err := wechat.V3ParseNotify(req)
	if err != nil {
		return "", false, fmt.Errorf("wechat parse notify: %w", err)
	}

	// Decrypt the payment notification resource using the API v3 key.
	result, err := notifyReq.DecryptPayCipherText(p.apiV3Key)
	if err != nil {
		return "", false, fmt.Errorf("wechat decrypt notify: %w", err)
	}

	if result.TradeState != "SUCCESS" {
		// Valid notification but not a success — acknowledge but don't process.
		return "", true, nil
	}

	if result.OutTradeNo == "" {
		return "", false, fmt.Errorf("wechat notify: missing out_trade_no")
	}
	return result.OutTradeNo, true, nil
}

// extractOpenID extracts the WeChat payer openid from order metadata (CallbackData field).
func extractOpenID(o *entity.PaymentOrder) string {
	if len(o.CallbackData) == 0 {
		return ""
	}
	var meta struct {
		OpenID string `json:"openid"`
	}
	if err := json.Unmarshal(o.CallbackData, &meta); err != nil {
		return ""
	}
	return meta.OpenID
}
