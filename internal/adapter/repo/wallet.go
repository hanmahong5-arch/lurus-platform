package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WalletRepo manages wallets, transactions, and payment orders.
type WalletRepo struct {
	db *gorm.DB
}

func NewWalletRepo(db *gorm.DB) *WalletRepo { return &WalletRepo{db: db} }

// GetOrCreate returns the wallet for an account, creating it if it doesn't exist.
func (r *WalletRepo) GetOrCreate(ctx context.Context, accountID int64) (*entity.Wallet, error) {
	var w entity.Wallet
	err := r.db.WithContext(ctx).
		Where(entity.Wallet{AccountID: accountID}).
		FirstOrCreate(&w).Error
	return &w, err
}

func (r *WalletRepo) GetByAccountID(ctx context.Context, accountID int64) (*entity.Wallet, error) {
	var w entity.Wallet
	err := r.db.WithContext(ctx).Where("account_id = ?", accountID).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &w, err
}

// Credit adds amount to balance and appends a ledger entry atomically.
// Balance arithmetic is performed in SQL (DECIMAL) to avoid float64 drift.
func (r *WalletRepo) Credit(ctx context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	var tx entity.WalletTransaction
	err := r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var w entity.Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("account_id = ?", accountID).First(&w).Error; err != nil {
			return fmt.Errorf("lock wallet: %w", err)
		}
		updates := map[string]any{
			"balance": gorm.Expr("balance + ?", amount),
		}
		if txType == entity.TxTypeTopup {
			updates["lifetime_topup"] = gorm.Expr("lifetime_topup + ?", amount)
		}
		if err := db.Model(&w).Updates(updates).Error; err != nil {
			return fmt.Errorf("update wallet: %w", err)
		}
		// Re-read to get DB-computed DECIMAL balance.
		if err := db.Where("id = ?", w.ID).First(&w).Error; err != nil {
			return fmt.Errorf("re-read wallet: %w", err)
		}
		tx = entity.WalletTransaction{
			WalletID:      w.ID,
			AccountID:     accountID,
			Type:          txType,
			Amount:        amount,
			BalanceAfter:  w.Balance,
			ProductID:     productID,
			ReferenceType: refType,
			ReferenceID:   refID,
			Description:   desc,
		}
		return db.Create(&tx).Error
	})
	return &tx, err
}

// Debit subtracts amount from balance (fails if insufficient funds).
// Uses conditional UPDATE with balance >= check for double-spend protection.
func (r *WalletRepo) Debit(ctx context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	var tx entity.WalletTransaction
	err := r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var w entity.Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("account_id = ?", accountID).First(&w).Error; err != nil {
			return fmt.Errorf("lock wallet: %w", err)
		}
		if w.Balance-w.Frozen < amount {
			return fmt.Errorf("insufficient available balance: have %.4f (%.4f balance - %.4f frozen), need %.4f",
				w.Balance-w.Frozen, w.Balance, w.Frozen, amount)
		}
		result := db.Model(&w).
			Where("id = ? AND (balance - frozen) >= ?", w.ID, amount).
			Updates(map[string]any{
				"balance":        gorm.Expr("balance - ?", amount),
				"lifetime_spend": gorm.Expr("lifetime_spend + ?", amount),
			})
		if result.Error != nil {
			return fmt.Errorf("update wallet: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("insufficient available balance: have %.4f (%.4f balance - %.4f frozen), need %.4f",
				w.Balance-w.Frozen, w.Balance, w.Frozen, amount)
		}
		// Re-read to get DB-computed DECIMAL balance.
		if err := db.Where("id = ?", w.ID).First(&w).Error; err != nil {
			return fmt.Errorf("re-read wallet: %w", err)
		}
		tx = entity.WalletTransaction{
			WalletID:      w.ID,
			AccountID:     accountID,
			Type:          txType,
			Amount:        -amount,
			BalanceAfter:  w.Balance,
			ProductID:     productID,
			ReferenceType: refType,
			ReferenceID:   refID,
			Description:   desc,
		}
		return db.Create(&tx).Error
	})
	return &tx, err
}

// ListTransactions returns paginated transactions for an account.
func (r *WalletRepo) ListTransactions(ctx context.Context, accountID int64, page, pageSize int) ([]entity.WalletTransaction, int64, error) {
	var list []entity.WalletTransaction
	var total int64
	q := r.db.WithContext(ctx).Model(&entity.WalletTransaction{}).Where("account_id = ?", accountID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := q.Order("id DESC").Limit(pageSize).Offset(offset).Find(&list).Error
	return list, total, err
}

// CreatePaymentOrder inserts a new pending order.
func (r *WalletRepo) CreatePaymentOrder(ctx context.Context, o *entity.PaymentOrder) error {
	return r.db.WithContext(ctx).Create(o).Error
}

// UpdatePaymentOrder updates the payment order status.
func (r *WalletRepo) UpdatePaymentOrder(ctx context.Context, o *entity.PaymentOrder) error {
	return r.db.WithContext(ctx).Save(o).Error
}

func (r *WalletRepo) GetPaymentOrderByNo(ctx context.Context, orderNo string) (*entity.PaymentOrder, error) {
	var o entity.PaymentOrder
	err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &o, err
}

func (r *WalletRepo) GetPaymentOrderByExternalID(ctx context.Context, externalID string) (*entity.PaymentOrder, error) {
	var o entity.PaymentOrder
	err := r.db.WithContext(ctx).Where("external_id = ?", externalID).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &o, err
}

// GetRedemptionCode returns a code, locked for update to prevent race conditions.
// ListOrders returns paginated payment orders for an account, newest first.
func (r *WalletRepo) ListOrders(ctx context.Context, accountID int64, page, pageSize int) ([]entity.PaymentOrder, int64, error) {
	var list []entity.PaymentOrder
	var total int64
	q := r.db.WithContext(ctx).Model(&entity.PaymentOrder{}).Where("account_id = ?", accountID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := q.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&list).Error
	return list, total, err
}

func (r *WalletRepo) GetRedemptionCode(ctx context.Context, code string) (*entity.RedemptionCode, error) {
	var rc entity.RedemptionCode
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("code = ?", code).First(&rc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &rc, err
}

func (r *WalletRepo) UpdateRedemptionCode(ctx context.Context, rc *entity.RedemptionCode) error {
	return r.db.WithContext(ctx).Save(rc).Error
}

func (r *WalletRepo) CreateRedemptionCode(ctx context.Context, rc *entity.RedemptionCode) error {
	return r.db.WithContext(ctx).Create(rc).Error
}

// BulkCreate inserts multiple redemption codes in batches of 100.
func (r *WalletRepo) BulkCreate(ctx context.Context, codes []entity.RedemptionCode) error {
	if len(codes) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(codes, 100).Error
}

// MarkPaymentOrderPaid atomically transitions a pending order to paid.
// Returns (order, didTransition, error). didTransition=true means this call
// performed the transition; false means the order was already non-pending (idempotent).
func (r *WalletRepo) MarkPaymentOrderPaid(ctx context.Context, orderNo string) (*entity.PaymentOrder, bool, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&entity.PaymentOrder{}).
		Where("order_no = ? AND status = ?", orderNo, entity.OrderStatusPending).
		Updates(map[string]any{"status": entity.OrderStatusPaid, "paid_at": now})
	if result.Error != nil {
		return nil, false, result.Error
	}

	var o entity.PaymentOrder
	if err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	return &o, result.RowsAffected > 0, nil
}

// RedeemCode validates and applies a redemption code inside a single transaction.
// Locks both the code row and the wallet row to prevent TOCTOU double-use.
func (r *WalletRepo) RedeemCode(ctx context.Context, accountID int64, code string) (*entity.WalletTransaction, error) {
	var tx entity.WalletTransaction
	err := r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		// 1. Lock the redemption code row.
		var rc entity.RedemptionCode
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("code = ?", code).First(&rc).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("invalid code")
			}
			return fmt.Errorf("lock code: %w", err)
		}

		// 2. Validate code.
		if rc.ExpiresAt != nil && rc.ExpiresAt.Before(time.Now()) {
			return fmt.Errorf("code has expired")
		}
		if rc.UsedCount >= rc.MaxUses {
			return fmt.Errorf("code has reached its usage limit")
		}
		if rc.RewardType != "credits" {
			return fmt.Errorf("unsupported reward type: %s", rc.RewardType)
		}

		// 3. Lock wallet and credit via SQL arithmetic.
		var w entity.Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("account_id = ?", accountID).First(&w).Error; err != nil {
			return fmt.Errorf("lock wallet: %w", err)
		}
		if err := db.Model(&w).Update("balance", gorm.Expr("balance + ?", rc.RewardValue)).Error; err != nil {
			return fmt.Errorf("credit wallet: %w", err)
		}
		if err := db.Where("id = ?", w.ID).First(&w).Error; err != nil {
			return fmt.Errorf("re-read wallet: %w", err)
		}

		// 4. Create ledger entry.
		tx = entity.WalletTransaction{
			WalletID:      w.ID,
			AccountID:     accountID,
			Type:          entity.TxTypeRedemption,
			Amount:        rc.RewardValue,
			BalanceAfter:  w.Balance,
			ReferenceType: "redemption_code",
			ReferenceID:   rc.Code,
			Description:   fmt.Sprintf("Redeem code %s", rc.Code),
			ProductID:     rc.ProductID,
		}
		if err := db.Create(&tx).Error; err != nil {
			return fmt.Errorf("create tx: %w", err)
		}

		// 5. Increment usage counter.
		rc.UsedCount++
		return db.Save(&rc).Error
	})
	return &tx, err
}

// ExpireStalePendingOrders marks pending orders older than maxAge as expired.
// Prefers expires_at when set; falls back to created_at + maxAge for legacy orders.
func (r *WalletRepo) ExpireStalePendingOrders(ctx context.Context, maxAge time.Duration) (int64, error) {
	now := time.Now().UTC()
	cutoff := now.Add(-maxAge)
	result := r.db.WithContext(ctx).
		Model(&entity.PaymentOrder{}).
		Where("status = ? AND ((expires_at IS NOT NULL AND expires_at < ?) OR (expires_at IS NULL AND created_at < ?))",
			entity.OrderStatusPending, now, cutoff).
		Update("status", entity.OrderStatusExpired)
	return result.RowsAffected, result.Error
}

// GetPendingOrderByIdempotencyKey returns a pending order matching the key, or nil.
func (r *WalletRepo) GetPendingOrderByIdempotencyKey(ctx context.Context, key string) (*entity.PaymentOrder, error) {
	var o entity.PaymentOrder
	err := r.db.WithContext(ctx).
		Where("idempotency_key = ? AND status = ?", key, entity.OrderStatusPending).
		First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &o, err
}

// CountActivePreAuths returns the number of active pre-authorizations for an account.
func (r *WalletRepo) CountActivePreAuths(ctx context.Context, accountID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entity.WalletPreAuthorization{}).
		Where("account_id = ? AND status = ?", accountID, entity.PreAuthStatusActive).
		Count(&count).Error
	return count, err
}

// CountPendingOrders returns the number of pending payment orders for an account.
func (r *WalletRepo) CountPendingOrders(ctx context.Context, accountID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entity.PaymentOrder{}).
		Where("account_id = ? AND status = ?", accountID, entity.OrderStatusPending).
		Count(&count).Error
	return count, err
}

// --- Pre-authorization methods ---

// CreatePreAuth inserts a new pre-authorization and freezes the amount on the wallet.
func (r *WalletRepo) CreatePreAuth(ctx context.Context, pa *entity.WalletPreAuthorization) error {
	return r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		// Lock wallet and freeze amount.
		var w entity.Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("account_id = ?", pa.AccountID).First(&w).Error; err != nil {
			return fmt.Errorf("lock wallet: %w", err)
		}
		if w.Balance-w.Frozen < pa.Amount {
			return fmt.Errorf("insufficient available balance: have %.4f, need %.4f (frozen: %.4f)",
				w.Balance, pa.Amount, w.Frozen)
		}
		if err := db.Model(&w).Update("frozen", gorm.Expr("frozen + ?", pa.Amount)).Error; err != nil {
			return fmt.Errorf("freeze amount: %w", err)
		}
		pa.WalletID = w.ID
		pa.Status = entity.PreAuthStatusActive
		return db.Create(pa).Error
	})
}

// GetPreAuthByID returns a pre-authorization by its ID.
func (r *WalletRepo) GetPreAuthByID(ctx context.Context, id int64) (*entity.WalletPreAuthorization, error) {
	var pa entity.WalletPreAuthorization
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&pa).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &pa, err
}

// GetPreAuthByReference returns a pre-authorization by product+reference pair.
func (r *WalletRepo) GetPreAuthByReference(ctx context.Context, productID, referenceID string) (*entity.WalletPreAuthorization, error) {
	var pa entity.WalletPreAuthorization
	err := r.db.WithContext(ctx).
		Where("product_id = ? AND reference_id = ?", productID, referenceID).
		First(&pa).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &pa, err
}

// SettlePreAuth atomically settles a pre-auth: charges actual_amount, unfreezes the hold,
// and creates a debit ledger entry.
func (r *WalletRepo) SettlePreAuth(ctx context.Context, id int64, actualAmount float64) (*entity.WalletPreAuthorization, error) {
	var pa entity.WalletPreAuthorization
	err := r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		// Lock the pre-auth row.
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ?", id, entity.PreAuthStatusActive).
			First(&pa).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("pre-auth %d not found or not active", id)
			}
			return fmt.Errorf("lock pre-auth: %w", err)
		}

		now := time.Now().UTC()
		pa.ActualAmount = &actualAmount
		pa.Status = entity.PreAuthStatusSettled
		pa.SettledAt = &now
		if err := db.Save(&pa).Error; err != nil {
			return fmt.Errorf("update pre-auth: %w", err)
		}

		// Lock wallet: unfreeze the held amount, debit actual amount.
		var w entity.Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", pa.WalletID).First(&w).Error; err != nil {
			return fmt.Errorf("lock wallet: %w", err)
		}

		// Unfreeze the original hold and debit actual amount.
		// WHERE balance >= actualAmount prevents negative balance.
		result := db.Model(&w).
			Where("balance >= ?", actualAmount).
			Updates(map[string]any{
				"frozen":         gorm.Expr("frozen - ?", pa.Amount),
				"balance":        gorm.Expr("balance - ?", actualAmount),
				"lifetime_spend": gorm.Expr("lifetime_spend + ?", actualAmount),
			})
		if result.Error != nil {
			return fmt.Errorf("settle wallet: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("insufficient balance to settle pre-auth %d: need %.4f, wallet %d",
				id, actualAmount, w.ID)
		}

		// Re-read for balance_after.
		if err := db.Where("id = ?", w.ID).First(&w).Error; err != nil {
			return fmt.Errorf("re-read wallet: %w", err)
		}

		// Ledger entry.
		tx := entity.WalletTransaction{
			WalletID:      w.ID,
			AccountID:     pa.AccountID,
			Type:          entity.TxTypePreAuthSettle,
			Amount:        -actualAmount,
			BalanceAfter:  w.Balance,
			ProductID:     pa.ProductID,
			ReferenceType: "pre_auth",
			ReferenceID:   fmt.Sprintf("%d", pa.ID),
			Description:   pa.Description,
		}
		return db.Create(&tx).Error
	})
	return &pa, err
}

// ReleasePreAuth atomically releases a pre-auth hold, returning the frozen amount.
func (r *WalletRepo) ReleasePreAuth(ctx context.Context, id int64) (*entity.WalletPreAuthorization, error) {
	var pa entity.WalletPreAuthorization
	err := r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ?", id, entity.PreAuthStatusActive).
			First(&pa).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("pre-auth %d not found or not active", id)
			}
			return fmt.Errorf("lock pre-auth: %w", err)
		}

		pa.Status = entity.PreAuthStatusReleased
		if err := db.Save(&pa).Error; err != nil {
			return fmt.Errorf("update pre-auth: %w", err)
		}

		// Unfreeze wallet.
		return db.Model(&entity.Wallet{}).Where("id = ?", pa.WalletID).
			Update("frozen", gorm.Expr("frozen - ?", pa.Amount)).Error
	})
	return &pa, err
}

// ExpireStalePreAuths marks active pre-auths past their expires_at as expired and unfreezes.
func (r *WalletRepo) ExpireStalePreAuths(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	var expired []entity.WalletPreAuthorization
	if err := r.db.WithContext(ctx).
		Where("status = ? AND expires_at < ?", entity.PreAuthStatusActive, now).
		Find(&expired).Error; err != nil {
		return 0, err
	}
	if len(expired) == 0 {
		return 0, nil
	}

	var count int64
	for _, pa := range expired {
		err := r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
			result := db.Model(&entity.WalletPreAuthorization{}).
				Where("id = ? AND status = ?", pa.ID, entity.PreAuthStatusActive).
				Update("status", entity.PreAuthStatusExpired)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil // already transitioned by another process
			}
			return db.Model(&entity.Wallet{}).Where("id = ?", pa.WalletID).
				Update("frozen", gorm.Expr("frozen - ?", pa.Amount)).Error
		})
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// --- Reconciliation ---

// FindPaidTopupOrdersWithoutCredit returns topup orders marked as paid
// that have no corresponding wallet_transaction (type=topup, reference_id=order_no).
// This detects cases where MarkOrderPaid succeeded but the Credit call failed.
func (r *WalletRepo) FindPaidTopupOrdersWithoutCredit(ctx context.Context) ([]entity.PaidOrderWithoutCredit, error) {
	var results []entity.PaidOrderWithoutCredit
	err := r.db.WithContext(ctx).Raw(`
		SELECT o.order_no, o.account_id, o.amount_cny, o.payment_method, o.paid_at
		FROM billing.payment_orders o
		LEFT JOIN billing.wallet_transactions t
			ON t.reference_type = 'payment_order'
			AND t.reference_id = o.order_no
			AND t.type = 'topup'
		WHERE o.status = 'paid'
			AND o.order_type = 'topup'
			AND t.id IS NULL
		ORDER BY o.paid_at DESC
		LIMIT 100
	`).Scan(&results).Error
	return results, err
}

// FindStalePendingOrders returns pending orders older than the given age
// that have NOT been expired yet (they still have time on their TTL or
// were created without expires_at). These are candidates for provider-side
// verification to detect missed webhooks.
func (r *WalletRepo) FindStalePendingOrders(ctx context.Context, minAge time.Duration) ([]entity.PaymentOrder, error) {
	cutoff := time.Now().UTC().Add(-minAge)
	var orders []entity.PaymentOrder
	err := r.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", entity.OrderStatusPending, cutoff).
		Order("created_at ASC").
		Limit(50).
		Find(&orders).Error
	return orders, err
}

// CreateReconciliationIssue persists a newly detected issue. Deduplicates by
// (issue_type, order_no, status='open') — returns nil without insert if duplicate.
func (r *WalletRepo) CreateReconciliationIssue(ctx context.Context, issue *entity.ReconciliationIssue) error {
	var count int64
	r.db.WithContext(ctx).Model(&entity.ReconciliationIssue{}).
		Where("issue_type = ? AND order_no = ? AND status = ?", issue.IssueType, issue.OrderNo, entity.ReconStatusOpen).
		Count(&count)
	if count > 0 {
		return nil // already tracked
	}
	return r.db.WithContext(ctx).Create(issue).Error
}

// ListReconciliationIssues returns paginated issues, optionally filtered by status.
func (r *WalletRepo) ListReconciliationIssues(ctx context.Context, status string, page, pageSize int) ([]entity.ReconciliationIssue, int64, error) {
	q := r.db.WithContext(ctx).Model(&entity.ReconciliationIssue{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []entity.ReconciliationIssue
	offset := (page - 1) * pageSize
	if err := q.Order("detected_at DESC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ResolveReconciliationIssue marks an issue as resolved or ignored.
func (r *WalletRepo) ResolveReconciliationIssue(ctx context.Context, id int64, status, resolution string) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&entity.ReconciliationIssue{}).
		Where("id = ? AND status = ?", id, entity.ReconStatusOpen).
		Updates(map[string]any{
			"status":      status,
			"resolution":  resolution,
			"resolved_at": &now,
		})
	if result.RowsAffected == 0 {
		return fmt.Errorf("issue %d not found or already resolved", id)
	}
	return result.Error
}

