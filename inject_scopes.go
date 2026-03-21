package main

import (
	"fmt"
	"os"
	"strings"
)

var scopes = map[string]string{
	"GetAccountByZitadelSub": "account:read",
	"GetAccountByID":         "account:read",
	"GetAccountByEmail":      "account:read",
	"GetAccountByPhone":      "account:read",
	"GetAccountByOAuth":      "account:read",
	"GetAccountOverview":     "account:read",
	"GetEntitlements":        "entitlement",
	"GetSubscription":        "entitlement",
	"UpsertAccount":          "account:write",
	"ValidateSession":        "account:read",
	"ReportUsage":            "wallet:read",
	"DebitWallet":            "wallet:debit",
	"GetWalletBalance":       "wallet:read",
	"GetBillingSummary":      "wallet:read",
	"PreAuthorize":           "wallet:debit",
	"SettlePreAuth":          "wallet:debit",
	"ReleasePreAuth":         "wallet:debit",
	"CreateCheckout":         "checkout",
	"GetCheckoutStatus":      "checkout",
	"GetPaymentMethods":      "checkout",
	"ExchangeLucToLut":       "wallet:debit",
	"GetCurrencyInfo":        "wallet:read",
}

func main() {
	data, _ := os.ReadFile(os.Args[1])
	content := string(data)
	count := 0

	for funcName, scope := range scopes {
		marker := "func (h *InternalHandler) " + funcName + "(c *gin.Context) {\n"
		scopeCheck := marker + "\tif !requireScope(c, \"" + scope + "\") {\n\t\treturn\n\t}\n"

		if strings.Contains(content, marker) && !strings.Contains(content, scopeCheck) {
			content = strings.Replace(content, marker, scopeCheck, 1)
			count++
			fmt.Printf("  + %s → %s\n", funcName, scope)
		}
	}

	os.WriteFile(os.Args[1], []byte(content), 0644)
	fmt.Printf("Injected %d scope checks\n", count)
}
