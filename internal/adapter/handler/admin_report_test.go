package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseDateParam_Empty(t *testing.T) {
	def := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := parseDateParam("", def)
	if err != nil {
		t.Fatalf("parseDateParam empty: %v", err)
	}
	if !got.Equal(def) {
		t.Errorf("got %v, want %v", got, def)
	}
}

func TestParseDateParam_Valid(t *testing.T) {
	got, err := parseDateParam("2026-03-01", time.Time{})
	if err != nil {
		t.Fatalf("parseDateParam valid: %v", err)
	}
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDateParam_Invalid(t *testing.T) {
	_, err := parseDateParam("not-a-date", time.Time{})
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestFinancialReport_BadDate(t *testing.T) {
	h := NewReportHandler(nil) // db is nil — we never reach SQL
	r := testRouter()
	r.GET("/admin/v1/reports/financial", h.FinancialReport)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/reports/financial?from=bad", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFinancialReport_ToBeforeFrom(t *testing.T) {
	h := NewReportHandler(nil) // db is nil — we never reach SQL
	r := testRouter()
	r.GET("/admin/v1/reports/financial", h.FinancialReport)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/reports/financial?from=2026-03-10&to=2026-03-01", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFinancialReport_BadToDate(t *testing.T) {
	h := NewReportHandler(nil)
	r := testRouter()
	r.GET("/admin/v1/reports/financial", h.FinancialReport)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/reports/financial?to=invalid", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
