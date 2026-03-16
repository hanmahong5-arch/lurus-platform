package repo

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
	gormcb "gorm.io/gorm/callbacks"
	"gorm.io/gorm/schema"
)

// jsonScanPool replaces the default FieldNewValuePool for json.RawMessage fields.
// SQLite returns TEXT columns as string, but database/sql can't scan string into
// *json.RawMessage (named []byte type). Using *interface{} accepts any driver type.
type jsonScanPool struct{}

func (jsonScanPool) Get() interface{} { return new(interface{}) }
func (jsonScanPool) Put(interface{}) {}

// patchJSONRawMessageFields finds all json.RawMessage fields in the schema and
// replaces their NewValuePool and Set function to handle SQLite's string return type.
func patchJSONRawMessageFields(fields []*schema.Field) {
	rawMsgType := reflect.TypeOf(json.RawMessage{})
	for _, field := range fields {
		if field.IndirectFieldType != rawMsgType {
			continue
		}
		oldSet := field.Set
		field.NewValuePool = jsonScanPool{}
		field.Set = func(ctx context.Context, value reflect.Value, v interface{}) error {
			if ptr, ok := v.(*interface{}); ok && ptr != nil {
				v = *ptr
			}
			switch val := v.(type) {
			case string:
				return oldSet(ctx, value, json.RawMessage(val))
			case []byte:
				return oldSet(ctx, value, json.RawMessage(val))
			case nil:
				return oldSet(ctx, value, json.RawMessage(nil))
			default:
				return oldSet(ctx, value, v)
			}
		}
	}
}

// setupTestDB creates an in-memory SQLite database with all entity tables.
// It strips schema prefixes (identity.*, billing.*) from cached table names,
// disables RETURNING, and patches json.RawMessage field scanning for SQLite.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	// Disable RETURNING for SQLite compatibility with json.RawMessage fields.
	// SQLite's RETURNING returns string type, but json.RawMessage expects []byte.
	// Without RETURNING, GORM falls back to last_insert_rowid() for auto-increment IDs.
	db.Callback().Create().Replace("gorm:create", gormcb.Create(&gormcb.Config{
		LastInsertIDReversed: true,
		CreateClauses:        []string{"INSERT", "VALUES", "ON CONFLICT"},
	}))

	// Strip schema prefix from GORM's cached table names for SQLite compatibility.
	// Entity TableName() returns "identity.accounts", "billing.wallets", etc.
	models := []interface{}{
		&entity.Account{}, &entity.OAuthBinding{},
		&entity.Wallet{}, &entity.WalletTransaction{},
		&entity.PaymentOrder{}, &entity.RedemptionCode{},
		&entity.Subscription{}, &entity.AccountEntitlement{},
		&entity.Product{}, &entity.ProductPlan{},
		&entity.AccountVIP{}, &entity.VIPLevelConfig{},
		&entity.Invoice{}, &entity.Refund{},
		&entity.ReferralRewardEvent{},
	}
	for _, m := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(m); err != nil {
			t.Fatalf("parse schema for %T: %v", m, err)
		}
		if idx := strings.Index(stmt.Schema.Table, "."); idx >= 0 {
			stmt.Schema.Table = stmt.Schema.Table[idx+1:]
		}
		patchJSONRawMessageFields(stmt.Schema.Fields)
	}

	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}

	return db
}
