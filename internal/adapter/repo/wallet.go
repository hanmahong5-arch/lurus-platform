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
		if w.Balance < amount {
			return fmt.Errorf("insufficient balance: have %.4f, need %.4f", w.Balance, amount)
		}
		result := db.Model(&w).
			Where("id = ? AND balance >= ?", w.ID, amount).
			Updates(map[string]any{
				"balance":        gorm.Expr("balance - ?", amount),
				"lifetime_spend": gorm.Expr("lifetime_spend + ?", amount),
			})
		if result.Error != nil {
			return fmt.Errorf("update wallet: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("insufficient balance: have %.4f, need %.4f", w.Balance, amount)
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
func (r *WalletRepo) ExpireStalePendingOrders(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	result := r.db.WithContext(ctx).
		Model(&entity.PaymentOrder{}).
		Where("status = ? AND created_at < ?", entity.OrderStatusPending, cutoff).
		Update("status", "expired")
	return result.RowsAffected, result.Error
}

