package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/kovaprov"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ---------- in-memory test doubles ----------

type fakeKovaOrgs struct {
	mu      sync.Mutex
	orgs    map[int64]*entity.Organization
	members map[int64]map[int64]*entity.OrgMember
}

func newFakeKovaOrgs() *fakeKovaOrgs {
	return &fakeKovaOrgs{
		orgs:    map[int64]*entity.Organization{},
		members: map[int64]map[int64]*entity.OrgMember{},
	}
}

func (f *fakeKovaOrgs) putOrg(o *entity.Organization) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *o
	f.orgs[o.ID] = &cp
}

func (f *fakeKovaOrgs) addMember(orgID, accountID int64, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.members[orgID] == nil {
		f.members[orgID] = map[int64]*entity.OrgMember{}
	}
	f.members[orgID][accountID] = &entity.OrgMember{
		OrgID: orgID, AccountID: accountID, Role: role,
	}
}

func (f *fakeKovaOrgs) GetByID(_ context.Context, id int64) (*entity.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orgs[id]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

func (f *fakeKovaOrgs) GetMember(_ context.Context, orgID, accountID int64) (*entity.OrgMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.members[orgID] == nil {
		return nil, nil
	}
	m, ok := f.members[orgID][accountID]
	if !ok {
		return nil, nil
	}
	cp := *m
	return &cp, nil
}

type fakeOrgServiceStore struct {
	mu     sync.Mutex
	rows   map[string]*entity.OrgService // key = "<orgID>/<service>"
	usages []*entity.UsageEvent
}

func newFakeOrgServiceStore() *fakeOrgServiceStore {
	return &fakeOrgServiceStore{rows: map[string]*entity.OrgService{}}
}

func (f *fakeOrgServiceStore) key(orgID int64, svc string) string {
	return svc + "/" + intStr(orgID)
}

func intStr(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(buf[i:])
	if neg {
		return "-" + s
	}
	return s
}

func (f *fakeOrgServiceStore) Get(_ context.Context, orgID int64, service string) (*entity.OrgService, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[f.key(orgID, service)]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (f *fakeOrgServiceStore) Upsert(_ context.Context, s *entity.OrgService) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.rows[f.key(s.OrgID, s.Service)] = &cp
	return nil
}

func (f *fakeOrgServiceStore) CreateUsageEvent(_ context.Context, ev *entity.UsageEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *ev
	cp.ID = int64(len(f.usages) + 1)
	f.usages = append(f.usages, &cp)
	ev.ID = cp.ID
	return nil
}

type stubProvisioner struct {
	resp     *kovaprov.ProvisionResponse
	err      error
	calls    int32
	isMock   bool
	lastReq  kovaprov.ProvisionRequest
	mu       sync.Mutex
}

func (s *stubProvisioner) Provision(_ context.Context, req kovaprov.ProvisionRequest) (*kovaprov.ProvisionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func (s *stubProvisioner) IsMock() bool { return s.isMock }

// ---------- tests ----------

func makeKovaSvc(t *testing.T) (*KovaProvisioningService, *fakeKovaOrgs, *fakeOrgServiceStore, *stubProvisioner) {
	t.Helper()
	orgs := newFakeKovaOrgs()
	store := newFakeOrgServiceStore()
	prov := &stubProvisioner{
		resp: &kovaprov.ProvisionResponse{
			TesterName: "acme",
			BaseURL:    "http://r6:3015",
			AdminKey:   "sk-kova-abcdef0123456789",
			Port:       3015,
		},
		isMock: true,
	}
	svc := NewKovaProvisioningService(orgs, store, store, prov)
	return svc, orgs, store, prov
}

func TestKovaProvisioning_Happy(t *testing.T) {
	svc, orgs, store, prov := makeKovaSvc(t)
	orgs.putOrg(&entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100})
	orgs.addMember(1, 100, "owner")

	res, err := svc.ProvisionKovaTester(context.Background(), 1)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if res.AdminKey != "sk-kova-abcdef0123456789" {
		t.Errorf("admin key not returned: %q", res.AdminKey)
	}
	if res.Service.Status != entity.OrgServiceStatusActive {
		t.Errorf("status=%s want active", res.Service.Status)
	}
	if res.Service.KeyHash == "" {
		t.Error("key hash should be set")
	}
	if res.Service.KeyPrefix != "sk-kova-" {
		t.Errorf("key prefix=%q want sk-kova-", res.Service.KeyPrefix)
	}
	row, _ := store.Get(context.Background(), 1, entity.OrgServiceKova)
	if row == nil {
		t.Fatal("row not persisted")
	}
	if row.BaseURL != "http://r6:3015" {
		t.Errorf("base url not persisted: %q", row.BaseURL)
	}
	if prov.lastReq.TesterName != "acme" {
		t.Errorf("provisioner saw tester=%q want acme", prov.lastReq.TesterName)
	}
}

func TestKovaProvisioning_IdempotentOnActive(t *testing.T) {
	svc, orgs, _, prov := makeKovaSvc(t)
	orgs.putOrg(&entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100})
	orgs.addMember(1, 100, "owner")

	if _, err := svc.ProvisionKovaTester(context.Background(), 1); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("first call should have invoked provisioner once, got %d", prov.calls)
	}

	res, err := svc.ProvisionKovaTester(context.Background(), 1)
	if err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if prov.calls != 1 {
		t.Errorf("idempotent call should NOT re-hit provisioner, got %d calls", prov.calls)
	}
	if res.AdminKey != "" {
		t.Errorf("idempotent response must not return admin_key, got %q", res.AdminKey)
	}
}

func TestKovaProvisioning_FailureRecorded(t *testing.T) {
	svc, orgs, store, prov := makeKovaSvc(t)
	orgs.putOrg(&entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100})
	prov.err = errors.New("boom")

	_, err := svc.ProvisionKovaTester(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error")
	}
	row, _ := store.Get(context.Background(), 1, entity.OrgServiceKova)
	if row == nil {
		t.Fatal("failure should have persisted a row")
	}
	if row.Status != entity.OrgServiceStatusFailed {
		t.Errorf("status=%s want failed", row.Status)
	}
	if msg, _ := row.Metadata["provisioner"].(map[string]any); msg == nil || msg["error"] == nil {
		t.Error("metadata.provisioner.error should carry the underlying error message")
	}
}

func TestKovaProvisioning_FailureKeepsExistingCredentials(t *testing.T) {
	svc, orgs, store, prov := makeKovaSvc(t)
	orgs.putOrg(&entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100})

	// First provision succeeds.
	if _, err := svc.ProvisionKovaTester(context.Background(), 1); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	originalRow, _ := store.Get(context.Background(), 1, entity.OrgServiceKova)
	originalKeyHash := originalRow.KeyHash

	// Force the row to look "failed" so the next call hits the upsert path.
	originalRow.Status = entity.OrgServiceStatusFailed
	_ = store.Upsert(context.Background(), originalRow)

	// Second provision — failure mode.
	prov.err = errors.New("transient r6 outage")
	_, err := svc.ProvisionKovaTester(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error")
	}

	row, _ := store.Get(context.Background(), 1, entity.OrgServiceKova)
	if row.KeyHash != originalKeyHash {
		t.Error("failure must preserve previously-stored key_hash so customer's existing key keeps working")
	}
}

func TestKovaProvisioning_OrgNotFound(t *testing.T) {
	svc, _, _, _ := makeKovaSvc(t)
	_, err := svc.ProvisionKovaTester(context.Background(), 999)
	if !errors.Is(err, ErrOrgNotFound) {
		t.Errorf("expected ErrOrgNotFound, got %v", err)
	}
}

func TestKovaProvisioning_RejectsBadOrgID(t *testing.T) {
	svc, _, _, _ := makeKovaSvc(t)
	if _, err := svc.ProvisionKovaTester(context.Background(), 0); err == nil {
		t.Error("expected error on org_id=0")
	}
	if _, err := svc.ProvisionKovaTester(context.Background(), -5); err == nil {
		t.Error("expected error on negative org_id")
	}
}

func TestKovaProvisioning_GetService_PermissionDenied(t *testing.T) {
	svc, orgs, store, _ := makeKovaSvc(t)
	orgs.putOrg(&entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100})
	_ = store.Upsert(context.Background(), &entity.OrgService{
		OrgID: 1, Service: entity.OrgServiceKova, Status: entity.OrgServiceStatusActive,
	})

	if _, err := svc.GetKovaService(context.Background(), 1, 999); !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("expected permission denied for non-member, got %v", err)
	}
}

func TestKovaProvisioning_GetService_NotProvisioned(t *testing.T) {
	svc, orgs, _, _ := makeKovaSvc(t)
	orgs.putOrg(&entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100})
	orgs.addMember(1, 100, "owner")

	if _, err := svc.GetKovaService(context.Background(), 1, 100); !errors.Is(err, ErrOrgServiceNotProvisioned) {
		t.Errorf("expected ErrOrgServiceNotProvisioned, got %v", err)
	}
}

func TestKovaProvisioning_RecordUsage(t *testing.T) {
	svc, _, store, _ := makeKovaSvc(t)
	ev := &entity.UsageEvent{
		OrgID: 1, TokensIn: 100, TokensOut: 50, CostMicros: 1234,
	}
	if err := svc.RecordUsage(context.Background(), ev); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if ev.ID == 0 {
		t.Error("event id should be assigned by store")
	}
	if ev.Service != entity.OrgServiceKova {
		t.Errorf("default service should be kova, got %q", ev.Service)
	}
	if ev.OccurredAt.IsZero() {
		t.Error("occurred_at should default to now when zero")
	}
	if len(store.usages) != 1 {
		t.Errorf("expected 1 stored event, got %d", len(store.usages))
	}
}

func TestKovaProvisioning_RecordUsage_RejectsBadInput(t *testing.T) {
	svc, _, _, _ := makeKovaSvc(t)
	cases := []*entity.UsageEvent{
		nil,
		{OrgID: 0, TokensIn: 1},
		{OrgID: 1, TokensIn: -1},
		{OrgID: 1, TokensOut: -1},
		{OrgID: 1, CostMicros: -1},
	}
	for i, ev := range cases {
		if err := svc.RecordUsage(context.Background(), ev); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}
