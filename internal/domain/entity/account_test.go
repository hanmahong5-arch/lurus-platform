package entity_test

import (
	"fmt"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestGenerateLurusID(t *testing.T) {
	tests := []struct {
		id   int64
		want string
	}{
		{1, "LU0000001"},
		{1000, "LU0001000"},
		{9999999, "LU9999999"},
		{10000000, "LU10000000"}, // overflow beyond 7 digits — still valid
	}
	for _, tc := range tests {
		got := entity.GenerateLurusID(tc.id)
		if got != tc.want {
			t.Errorf("GenerateLurusID(%d) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestAccountIsActive(t *testing.T) {
	tests := []struct {
		status int16
		active bool
	}{
		{entity.AccountStatusActive, true},
		{entity.AccountStatusSuspended, false},
		{entity.AccountStatusDeleted, false},
	}
	for _, tc := range tests {
		a := &entity.Account{Status: tc.status}
		if got := a.IsActive(); got != tc.active {
			t.Errorf("status=%d IsActive()=%v, want %v", tc.status, got, tc.active)
		}
	}
}

func TestLurusIDFormat(t *testing.T) {
	// All generated IDs must start with "LU" and contain only digits after
	for _, id := range []int64{1, 42, 1000000, 9999999} {
		lid := entity.GenerateLurusID(id)
		if len(lid) < 3 || lid[:2] != "LU" {
			t.Errorf("GenerateLurusID(%d)=%q: expected prefix LU", id, lid)
		}
		for _, ch := range lid[2:] {
			if ch < '0' || ch > '9' {
				t.Errorf("GenerateLurusID(%d)=%q: non-digit char %q after LU", id, lid, ch)
			}
		}
	}
}

func TestAccountTableName(t *testing.T) {
	a := entity.Account{}
	if got := a.TableName(); got != "identity.accounts" {
		t.Errorf("TableName()=%q, want identity.accounts", got)
	}
}

func TestGenerateLurusIDUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := int64(1); i <= 1000; i++ {
		lid := entity.GenerateLurusID(i)
		key := fmt.Sprintf("%d→%s", i, lid)
		if seen[lid] {
			t.Errorf("duplicate LurusID %q at id=%d", lid, i)
		}
		seen[lid] = true
		_ = key
	}
}
