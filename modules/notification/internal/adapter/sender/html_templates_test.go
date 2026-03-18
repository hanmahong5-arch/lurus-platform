package sender

import (
	"strings"
	"testing"
)

func TestRenderHTMLEmail_ContainsBranding(t *testing.T) {
	html, err := RenderHTMLEmail("<p>Test content</p>")
	if err != nil {
		t.Fatalf("RenderHTMLEmail error: %v", err)
	}
	if !strings.Contains(html, "LURUS") {
		t.Error("rendered HTML should contain brand name")
	}
	if !strings.Contains(html, "Test content") {
		t.Error("rendered HTML should contain the content")
	}
	if !strings.Contains(html, "Manage notification preferences") {
		t.Error("rendered HTML should contain unsubscribe link")
	}
}

func TestGetHTMLContent_KnownTemplate(t *testing.T) {
	html := GetHTMLContent("welcome", nil)
	if html == "" {
		t.Fatal("welcome template should return non-empty HTML")
	}
	if !strings.Contains(html, "Welcome to Lurus") {
		t.Error("welcome template should contain welcome message")
	}
}

func TestGetHTMLContent_WithVars(t *testing.T) {
	html := GetHTMLContent("quota_warning", map[string]string{
		"percent":   "80",
		"remaining": "20000",
	})
	if html == "" {
		t.Fatal("quota_warning template should return non-empty HTML")
	}
	if !strings.Contains(html, "80%") {
		t.Error("should contain percent value")
	}
	if !strings.Contains(html, "20000") {
		t.Error("should contain remaining value")
	}
}

func TestGetHTMLContent_UnknownTemplate(t *testing.T) {
	html := GetHTMLContent("nonexistent", nil)
	if html != "" {
		t.Error("unknown template should return empty string")
	}
}

func TestGetHTMLContent_XSSPrevention(t *testing.T) {
	html := GetHTMLContent("quota_warning", map[string]string{
		"percent":   "<script>alert('xss')</script>",
		"remaining": "100",
	})
	if strings.Contains(html, "<script>") {
		t.Error("HTML should escape script tags in variables")
	}
}
