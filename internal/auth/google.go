package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// GoogleTokenInfo represents payload returned from Google's tokeninfo API.
type GoogleTokenInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Aud           string `json:"aud"`
	Error         string `json:"error"`
	ErrorDesc     string `json:"error_description"`
}

type googleAuthRequest struct {
	Token      string `json:"token"`
	IDToken    string `json:"id_token"`
	Credential string `json:"credential"`
}

// verifyGoogleIDToken verifies a Google ID token via Google's tokeninfo endpoint.
func verifyGoogleIDToken(token string) (*GoogleTokenInfo, error) {
	if token == "" {
		return nil, fmt.Errorf("token is empty")
	}

	endpoint := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(token)
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to contact google tokeninfo endpoint: %w", err)
	}
	defer resp.Body.Close()

	var info GoogleTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode google tokeninfo response: %w", err)
	}

	if info.Error != "" || info.Sub == "" {
		return nil, fmt.Errorf("invalid google token: %s (%s)", info.Error, info.ErrorDesc)
	}

	return &info, nil
}

// processGoogleUser authenticates or registers a user from verified Google profile.
func processGoogleUser(w http.ResponseWriter, info *GoogleTokenInfo) (*User, *Session, error) {
	email := strings.TrimSpace(strings.ToLower(info.Email))
	googleID := strings.TrimSpace(info.Sub)
	name := strings.TrimSpace(info.Name)
	if name == "" {
		name = strings.TrimSpace(info.GivenName + " " + info.FamilyName)
	}
	if name == "" {
		name = "Google User"
	}
	picture := strings.TrimSpace(info.Picture)

	var user *User
	var isNewUser bool

	// 1. Search by Google ID
	if existing, ok := findUserByGoogleID(googleID); ok {
		user = existing
		if picture != "" && user.AvatarURL != picture {
			user.AvatarURL = picture
			user.UpdatedAt = time.Now()
			_ = saveUser(user)
		}
	} else if existing, ok := findUserByEmail(email); ok {
		// 2. Search by Email - Link Google account to existing user
		user = existing
		user.GoogleID = googleID
		if picture != "" && user.AvatarURL == "" {
			user.AvatarURL = picture
		}
		user.UpdatedAt = time.Now()
		_ = saveUser(user)
	} else {
		// 3. Register New User
		isNewUser = true
		parts := strings.Split(email, "@")
		reg := regexp.MustCompile("[^a-zA-Z0-9_-]")
		username := reg.ReplaceAllString(parts[0], "")

		idBytes := make([]byte, 16)
		_, _ = rand.Read(idBytes)
		userID := "usr_" + hex.EncodeToString(idBytes)[:8]

		user = &User{
			ID:        userID,
			Email:     email,
			Name:      name,
			Username:  username,
			GoogleID:  googleID,
			AvatarURL: picture,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := saveUser(user); err != nil {
			return nil, nil, fmt.Errorf("failed to save new google user: %w", err)
		}

		// Asynchronously send Welcome Email for newly registered users
		go sendResendWelcomeEmail(user.Email, user.Name, user.Username)
	}

	session, err := createSessionAndSetCookie(w, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}

	_ = isNewUser

	return user, session, nil
}

// handleGoogleAuth processes Google ID Token verification requests from SPA/mobile apps.
func handleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req googleAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token := req.Token
	if token == "" {
		token = req.IDToken
	}
	if token == "" {
		token = req.Credential
	}

	if token == "" {
		writeJSONError(w, http.StatusBadRequest, "google token is required")
		return
	}

	info, err := verifyGoogleIDToken(token)
	if err != nil {
		log.Printf("auth: google token verification failed: %v", err)
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired google token")
		return
	}

	user, session, err := processGoogleUser(w, info)
	if err != nil {
		log.Printf("auth: process google user failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	pub := user.ToPublic()
	_ = writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":     session.Token,
		"id":        pub.ID,
		"name":      pub.Name,
		"email":     pub.Email,
		"username":  pub.Username,
		"avatar_url": pub.AvatarURL,
		"google_id": pub.GoogleID,
		"createdAt": pub.CreatedAt,
		"user":      pub,
	})
}

// handleGoogleOAuthLogin initiates the Google OAuth authorization code redirect flow.
func handleGoogleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	redirectURI := os.Getenv("GOOGLE_REDIRECT_URI")

	if clientID == "" {
		writeJSONError(w, http.StatusInternalServerError, "GOOGLE_CLIENT_ID environment variable is not configured")
		return
	}

	if redirectURI == "" {
		frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
		if frontendOrigin == "" {
			frontendOrigin = "http://localhost:8080"
		}
		redirectURI = strings.TrimSuffix(frontendOrigin, "/") + "/api/auth/google/callback"
	}

	stateBytes := make([]byte, 16)
	_, _ = rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)

	u, _ := url.Parse("https://accounts.google.com/o/oauth2/v2/auth")
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("prompt", "select_account")
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusTemporaryRedirect)
}

// handleGoogleOAuthCallback handles Google OAuth redirect callback, exchanges code, and establishes user session.
func handleGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSONError(w, http.StatusBadRequest, "missing authorization code")
		return
	}

	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURI := os.Getenv("GOOGLE_REDIRECT_URI")

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "https://www.enlazer.com.ng"
	}
	frontendOrigin = strings.TrimSuffix(frontendOrigin, "/")

	if redirectURI == "" {
		redirectURI = frontendOrigin + "/api/auth/google/callback"
	}

	// Exchange Code for Access Token / ID Token
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		log.Printf("auth: google token exchange failed: %v", err)
		http.Redirect(w, r, frontendOrigin+"/login?error=token_exchange_failed", http.StatusTemporaryRedirect)
		return
	}
	defer resp.Body.Close()

	var tokenRes struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil || tokenRes.IDToken == "" {
		log.Printf("auth: failed to decode google token response or missing id_token: %v", err)
		http.Redirect(w, r, frontendOrigin+"/login?error=invalid_token_response", http.StatusTemporaryRedirect)
		return
	}

	info, err := verifyGoogleIDToken(tokenRes.IDToken)
	if err != nil {
		log.Printf("auth: google callback id_token verification failed: %v", err)
		http.Redirect(w, r, frontendOrigin+"/login?error=token_verification_failed", http.StatusTemporaryRedirect)
		return
	}

	_, session, err := processGoogleUser(w, info)
	if err != nil {
		log.Printf("auth: failed processing google user: %v", err)
		http.Redirect(w, r, frontendOrigin+"/login?error=user_processing_failed", http.StatusTemporaryRedirect)
		return
	}

	// Redirect back to frontend dashboard with session token
	targetRedirect := fmt.Sprintf("%s/dashboard?token=%s", frontendOrigin, session.Token)
	http.Redirect(w, r, targetRedirect, http.StatusTemporaryRedirect)
}

// HandleGoogleAuth exports handleGoogleAuth handler method on auth Service.
func (s *Service) HandleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	handleGoogleAuth(w, r)
}

// HandleGoogleOAuthLogin exports handleGoogleOAuthLogin handler method on auth Service.
func (s *Service) HandleGoogleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	handleGoogleOAuthLogin(w, r)
}

// HandleGoogleOAuthCallback exports handleGoogleOAuthCallback handler method on auth Service.
func (s *Service) HandleGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	handleGoogleOAuthCallback(w, r)
}
