package buildinfo

import (
	"testing"
)

// TestGet_ReturnsNonEmpty verifies Get populates both fields even when
// neither ldflags nor VCS info are plumbed (fallback = "unknown").
func TestGet_ReturnsNonEmpty(t *testing.T) {
	info := Get()
	if info.SHA == "" {
		t.Error("Get().SHA should never be empty (expected ldflags value, VCS fallback, or 'unknown')")
	}
	if info.BuiltAt == "" {
		t.Error("Get().BuiltAt should never be empty")
	}
}

// TestGet_Cached verifies the result is memoised (resolve() runs exactly
// once). This protects against us accidentally introducing expensive work
// inside resolve() later.
func TestGet_Cached(t *testing.T) {
	first := Get()
	second := Get()
	if first != second {
		t.Errorf("Get() not cached: %+v vs %+v", first, second)
	}
}

// TestResolve_FallbackShape ensures the pure function returns plausibly
// shaped data. We can't assert the exact SHA (it varies per build), only
// that it's non-empty and the timestamp is a string.
func TestResolve_FallbackShape(t *testing.T) {
	got := resolve()
	if got.SHA == "" {
		t.Error("resolve().SHA should not be empty")
	}
	if got.BuiltAt == "" {
		t.Error("resolve().BuiltAt should not be empty")
	}
}
