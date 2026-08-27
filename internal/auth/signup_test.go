package auth

import (
	"strings"
	"testing"
)

func TestBuildWelcomeEmailHTML(t *testing.T) {
	userName := "Precious Onuigbo"
	username := "precious"
	profileURL := "https://capstone-project.name.ng/@precious"
	dashboardURL := "https://capstone-project.name.ng/dashboard"

	html := buildWelcomeEmailHTML(userName, username, profileURL, dashboardURL)

	if !strings.Contains(html, "Welcome to Bloom, Precious Onuigbo!") {
		t.Errorf("expected HTML to contain welcome greeting for user")
	}

	if !strings.Contains(html, "@precious") {
		t.Errorf("expected HTML to contain handle @precious")
	}

	if !strings.Contains(html, profileURL) {
		t.Errorf("expected HTML to contain profile URL %s", profileURL)
	}

	if !strings.Contains(html, dashboardURL) {
		t.Errorf("expected HTML to contain dashboard URL %s", dashboardURL)
	}
}
