package entity_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestSubscriptionIsLive(t *testing.T) {
	tests := []struct {
		status string
		live   bool
	}{
		{entity.SubStatusActive, true},
		{entity.SubStatusGrace, true},
		{entity.SubStatusTrial, true},
		{entity.SubStatusPending, false},
		{entity.SubStatusExpired, false},
		{entity.SubStatusCancelled, false},
		{entity.SubStatusSuspended, false},
	}
	for _, tc := range tests {
		s := &entity.Subscription{Status: tc.status}
		if got := s.IsLive(); got != tc.live {
			t.Errorf("status=%q IsLive()=%v, want %v", tc.status, got, tc.live)
		}
	}
}

func TestSubscriptionTableName(t *testing.T) {
	s := entity.Subscription{}
	if got := s.TableName(); got != "identity.subscriptions" {
		t.Errorf("TableName()=%q, want identity.subscriptions", got)
	}
}

func TestEntitlementTableName(t *testing.T) {
	e := entity.AccountEntitlement{}
	if got := e.TableName(); got != "identity.account_entitlements" {
		t.Errorf("TableName()=%q, want identity.account_entitlements", got)
	}
}
