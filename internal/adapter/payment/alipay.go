package payment

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-pay/gopay"
	"github.com/go-pay/gopay/alipay"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// AlipayProvider implements Provider, Refunder, and NotifyHandler for direct Alipay integration.
type AlipayProvider struct {
	client               *alipay.Client
	notifyURL            string
	returnURL            string
	tradeType            TradeType
	alipayPublicCertData []byte // stored for async notification verification
}

// AlipayConfig holds configuration for direct Alipay integration.
type AlipayConfig struct {
	AppID      string // ALIPAY_APP_ID
	PrivateKey string // ALIPAY_PRIVATE_KEY (RSA2, PKCS1 format)
	IsProd     bool   // ALIPAY_IS_PROD (true = production, false = sandbox)
	NotifyURL  string // ALIPAY_NOTIFY_URL (async callback URL)
	ReturnURL  string // ALIPAY_RETURN_URL (sync redirect URL)
	// Certificate mode — set all three for cert-based verification (recommended).
	AppPublicCertContent    []byte // ALIPAY_APP_PUBLIC_CERT content
	AlipayPublicCertContent []byte // ALIPAY_PUBLIC_CERT content
	AlipayRootCertContent   []byte // ALIPAY_ROOT_CERT content
}

// NewAlipayProvider creates an Alipay provider using go-pay.
// Returns (nil, nil) if AppID or PrivateKey is empty (feature disabled).
func NewAlipayProvider(cfg AlipayConfig, tradeType TradeType) (*AlipayProvider, error) {
	if cfg.AppID == "" || cfg.PrivateKey == "" {
		return nil, nil
	}

	client, err := alipay.NewClient(cfg.AppID, cfg.PrivateKey, cfg.IsProd)
	if err != nil {
		return nil, fmt.Errorf("alipay new client: %w", err)
	}

	// Certificate-based signature verification (recommended for production).
	if len(cfg.AppPublicCertContent) > 0 && len(cfg.AlipayPublicCertContent) > 0 && len(cfg.AlipayRootCertContent) > 0 {
		if err := client.SetCertSnByContent(cfg.AppPublicCertContent, cfg.AlipayPublicCertContent, cfg.AlipayRootCertContent); err != nil {
			return nil, fmt.Errorf("alipay set cert: %w", err)
		}
	}

	return &AlipayProvider{
		client:               client,
		notifyURL:            cfg.NotifyURL,
		returnURL:            cfg.ReturnURL,
		tradeType:            tradeType,
		alipayPublicCertData: cfg.AlipayPublicCertContent,
	}, nil
}

// Name returns the provider identifier.
func (p *AlipayProvider) Name() string {
	return "alipay"
}

// CreateCheckout creates an Alipay payment and returns the pay URL or QR code URL.
func (p *AlipayProvider) CreateCheckout(ctx context.Context, o *entity.PaymentOrder, returnURL string) (payURL, externalID string, err error) {
	if returnURL == "" {
		returnURL = p.returnURL
	}

	bm := gopay.BodyMap{}
	bm.Set("subject", fmt.Sprintf("Lurus 充值 %.2f CNY", o.AmountCNY))
	bm.Set("out_trade_no", o.OrderNo)
	bm.Set("total_amount", fmt.Sprintf("%.2f", o.AmountCNY))
	bm.Set("product_code", p.productCode())

	tradeType := p.tradeType
	// Allow per-order override via PaymentMethod field.
	switch o.PaymentMethod {
	case "alipay_wap":
		tradeType = TradeTypeWAP
	case "alipay_qr":
		tradeType = TradeTypeNative
	}

	switch tradeType {
	case TradeTypeWAP:
		bm.Set("quit_url", returnURL)
		result, err := p.client.TradeWapPay(ctx, bm)
		if err != nil {
			return "", "", fmt.Errorf("alipay wap pay: %w", err)
		}
		return result, o.OrderNo, nil

	case TradeTypeNative:
		bm.Set("notify_url", p.notifyURL)
		resp, err := p.client.TradePrecreate(ctx, bm)
		if err != nil {
			return "", "", fmt.Errorf("alipay precreate: %w", err)
		}
		if resp.Response.QrCode == "" {
			return "", "", fmt.Errorf("alipay precreate: empty qr_code, sub_msg=%s", resp.Response.SubMsg)
		}
		return resp.Response.QrCode, o.OrderNo, nil

	default: // TradeTypePC
		bm.Set("notify_url", p.notifyURL)
		bm.Set("return_url", returnURL)
		result, err := p.client.TradePagePay(ctx, bm)
		if err != nil {
			return "", "", fmt.Errorf("alipay page pay: %w", err)
		}
		return result, o.OrderNo, nil
	}
}

// Refund initiates a refund at Alipay.
func (p *AlipayProvider) Refund(ctx context.Context, refundNo, orderNo string, refundAmount, _ float64) error {
	bm := gopay.BodyMap{}
	bm.Set("out_trade_no", orderNo)
	bm.Set("refund_amount", fmt.Sprintf("%.2f", refundAmount))
	bm.Set("out_request_no", refundNo)

	resp, err := p.client.TradeRefund(ctx, bm)
	if err != nil {
		return fmt.Errorf("alipay refund: %w", err)
	}
	if resp.Response.FundChange != "Y" {
		return fmt.Errorf("alipay refund: no fund change, sub_code=%s sub_msg=%s", resp.Response.SubCode, resp.Response.SubMsg)
	}
	slog.Info("alipay/refund", "refund_no", refundNo, "order_no", orderNo, "amount", refundAmount)
	return nil
}

// HandleNotify parses and verifies an Alipay async notification.
func (p *AlipayProvider) HandleNotify(req *http.Request) (orderNo string, ok bool, err error) {
	notifyReq, err := alipay.ParseNotifyToBodyMap(req)
	if err != nil {
		return "", false, fmt.Errorf("alipay parse notify: %w", err)
	}

	// Verify signature using the Alipay public cert.
	if len(p.alipayPublicCertData) > 0 {
		if ok, err := alipay.VerifySignWithCert(p.alipayPublicCertData, notifyReq); err != nil || !ok {
			return "", false, fmt.Errorf("alipay notify signature invalid: %w", err)
		}
	}

	tradeStatus := notifyReq.Get("trade_status")
	if tradeStatus != "TRADE_SUCCESS" && tradeStatus != "TRADE_FINISHED" {
		// Valid notification but not a success event — acknowledge but don't process.
		return "", true, nil
	}

	outTradeNo := notifyReq.Get("out_trade_no")
	if outTradeNo == "" {
		return "", false, fmt.Errorf("alipay notify: missing out_trade_no")
	}
	return outTradeNo, true, nil
}

// QueryOrder checks the payment status of an order at Alipay via TradeQuery.
func (p *AlipayProvider) QueryOrder(ctx context.Context, orderNo string) (*OrderQueryResult, error) {
	bm := gopay.BodyMap{}
	bm.Set("out_trade_no", orderNo)

	resp, err := p.client.TradeQuery(ctx, bm)
	if err != nil {
		return nil, fmt.Errorf("alipay trade query: %w", err)
	}
	paid := resp.Response.TradeStatus == "TRADE_SUCCESS" || resp.Response.TradeStatus == "TRADE_FINISHED"
	var amount float64
	if paid {
		_, _ = fmt.Sscanf(resp.Response.TotalAmount, "%f", &amount)
	}
	return &OrderQueryResult{Paid: paid, Amount: amount}, nil
}

// productCode returns the Alipay product code for the trade type.
func (p *AlipayProvider) productCode() string {
	switch p.tradeType {
	case TradeTypeWAP:
		return "QUICK_WAP_WAY"
	case TradeTypeNative:
		return "FACE_TO_FACE_PAYMENT"
	default:
		return "FAST_INSTANT_TRADE_PAY"
	}
}
