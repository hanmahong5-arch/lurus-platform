package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// fakeNotifSvc is a hand-rolled stub of notificationSvc used to verify the
// HTTP envelope shape without touching gorm/postgres/redis.
type fakeNotifSvc struct {
	listItems     []entity.Notification
	listTotal     int64
	listLimit     int
	listOffset    int
	listFilter    app.ListFilter
	breakdown     app.UnreadBreakdown
	breakdownErr  error
}

func (f *fakeNotifSvc) ListByAccountFiltered(_ context.Context, _ int64, ff app.ListFilter, limit, offset int) ([]entity.Notification, int64, error) {
	f.listFilter = ff
	f.listLimit = limit
	f.listOffset = offset
	return f.listItems, f.listTotal, nil
}
func (f *fakeNotifSvc) CountUnread(_ context.Context, _ int64) (int64, error) {
	return f.breakdown.Total, nil
}
func (f *fakeNotifSvc) CountUnreadBreakdown(_ context.Context, _ int64) (app.UnreadBreakdown, error) {
	return f.breakdown, f.breakdownErr
}
func (f *fakeNotifSvc) MarkRead(_ context.Context, _, _ int64) error          { return nil }
func (f *fakeNotifSvc) MarkAllRead(_ context.Context, _ int64) (int64, error) { return 0, nil }
func (f *fakeNotifSvc) Send(_ context.Context, _ app.SendRequest) error       { return nil }

func newTestHandler(svc notificationSvc) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

func setAccountID(c *gin.Context, accountID int64) {
	c.Set("account_id", accountID)
}

func runHandler(handler func(*gin.Context), method, target string, accountID int64) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	setAccountID(c, accountID)
	handler(c)
	return w
}

func TestList_NewEnvelopeShape(t *testing.T) {
	svc := &fakeNotifSvc{
		listItems: []entity.Notification{
			{ID: 1, AccountID: 7, Source: "lucrum", Category: "strategy", Title: "T", Body: "B", CreatedAt: time.Now()},
		},
		listTotal: 1,
	}
	h := newTestHandler(svc)

	w := runHandler(h.List, http.MethodGet, "/api/v1/notifications?page=1&limit=20", 7)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// New shape: {data: [...], meta: {total, limit, offset}}.
	if _, ok := got["data"]; !ok {
		t.Errorf("response missing `data` key: %v", got)
	}
	if _, ok := got["meta"]; !ok {
		t.Errorf("response missing `meta` key: %v", got)
	}
	if _, ok := got["items"]; ok {
		t.Errorf("response still has legacy `items` key (breaking change incomplete): %v", got)
	}

	meta, ok := got["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta is not an object: %v", got["meta"])
	}
	if total, _ := meta["total"].(float64); int(total) != 1 {
		t.Errorf("meta.total = %v, want 1", meta["total"])
	}
}

func TestList_PageQueryTranslatesToOffset(t *testing.T) {
	svc := &fakeNotifSvc{}
	h := newTestHandler(svc)

	runHandler(h.List, http.MethodGet, "/api/v1/notifications?page=3&limit=20", 7)
	if svc.listOffset != 40 {
		t.Errorf("offset for page=3,limit=20 = %d, want 40", svc.listOffset)
	}
	if svc.listLimit != 20 {
		t.Errorf("limit = %d, want 20", svc.listLimit)
	}
}

func TestList_OffsetQueryStillWorks(t *testing.T) {
	svc := &fakeNotifSvc{}
	h := newTestHandler(svc)

	runHandler(h.List, http.MethodGet, "/api/v1/notifications?limit=10&offset=30", 7)
	if svc.listOffset != 30 || svc.listLimit != 10 {
		t.Errorf("offset/limit = %d/%d, want 30/10", svc.listOffset, svc.listLimit)
	}
}

func TestList_FiltersPassThrough(t *testing.T) {
	svc := &fakeNotifSvc{}
	h := newTestHandler(svc)

	runHandler(h.List, http.MethodGet, "/api/v1/notifications?source=lucrum&category=strategy&unread_only=true", 7)
	if svc.listFilter.Source != "lucrum" {
		t.Errorf("filter.Source = %q, want lucrum", svc.listFilter.Source)
	}
	if svc.listFilter.Category != "strategy" {
		t.Errorf("filter.Category = %q, want strategy", svc.listFilter.Category)
	}
	if !svc.listFilter.UnreadOnly {
		t.Errorf("filter.UnreadOnly = false, want true")
	}
}

func TestUnread_NewBreakdownShape_Populated(t *testing.T) {
	svc := &fakeNotifSvc{
		breakdown: app.UnreadBreakdown{
			Total:      5,
			BySource:   map[string]int64{"identity": 2, "lucrum": 3},
			ByCategory: map[string]int64{"account": 2, "strategy": 3},
		},
	}
	h := newTestHandler(svc)

	w := runHandler(h.Unread, http.MethodGet, "/api/v1/notifications/unread", 7)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"total", "by_source", "by_category"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in response: %v", key, got)
		}
	}
	if total, _ := got["total"].(float64); int(total) != 5 {
		t.Errorf("total = %v, want 5", got["total"])
	}
	bySource, _ := got["by_source"].(map[string]any)
	if v, _ := bySource["lucrum"].(float64); int(v) != 3 {
		t.Errorf("by_source.lucrum = %v, want 3", bySource["lucrum"])
	}

	if strings.Contains(w.Body.String(), `"unread"`) {
		t.Errorf("response still has legacy `unread` key (breaking change incomplete): %s", w.Body.String())
	}
}

func TestUnread_NewBreakdownShape_EmptyMaps(t *testing.T) {
	// User has zero notifications: by_source and by_category must be `{}` (not null).
	svc := &fakeNotifSvc{
		breakdown: app.UnreadBreakdown{
			Total:      0,
			BySource:   map[string]int64{},
			ByCategory: map[string]int64{},
		},
	}
	h := newTestHandler(svc)

	w := runHandler(h.Unread, http.MethodGet, "/api/v1/notifications/unread", 7)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Must serialize empty maps as `{}` not `null`.
	if !strings.Contains(body, `"by_source":{}`) {
		t.Errorf("by_source not serialized as {}: %s", body)
	}
	if !strings.Contains(body, `"by_category":{}`) {
		t.Errorf("by_category not serialized as {}: %s", body)
	}
}

func TestUnread_NilMapsCoercedToEmpty(t *testing.T) {
	// Even when the service hands back nil maps (defensive case), the handler
	// must replace them with empty maps so the JSON shape stays stable.
	svc := &fakeNotifSvc{
		breakdown: app.UnreadBreakdown{Total: 0, BySource: nil, ByCategory: nil},
	}
	h := newTestHandler(svc)

	w := runHandler(h.Unread, http.MethodGet, "/api/v1/notifications/unread", 7)
	body := w.Body.String()
	if !strings.Contains(body, `"by_source":{}`) {
		t.Errorf("nil by_source should serialize as {}: %s", body)
	}
	if !strings.Contains(body, `"by_category":{}`) {
		t.Errorf("nil by_category should serialize as {}: %s", body)
	}
}
