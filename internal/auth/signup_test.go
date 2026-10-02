package auth

import (
	"strings"
	"testing"
)

func TestRenderWelcomeTemplate(t *testing.T) {
	fullName := "Precious Onuigbo"
	leadName := "Precious"
	username := "precious"
	profileURL := "https://enlazer.cloud/@precious"
	dashboardURL := "https://enlazer.cloud/dashboard"
	avatarURL := "https://enlazer.cloud/default-avatar.png"
	subject := "Welcome to Enlazer, Precious! Your Digital Card is Ready"
	preheader := "Your account is live and your digital card profile is ready."

	html := renderWelcomeTemplate(map[string]string{
		"subject":       subject,
		"preheader":     preheader,
		"FULL_NAME":     fullName,
		"lead_Name":     leadName,
		"HANDLE":        username,
		"AVATAR_URL":    avatarURL,
		"PROFILE_URL":   profileURL,
		"DASHBOARD_URL": dashboardURL,
	})

	if !strings.Contains(html, "Welcome to Enlazer, Precious!") {
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
