package main

import (
	"bufio"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"

	_ "github.com/lib/pq"

	"github.com/onuigboprecious/infarbloom/backend/internal/analytics"
	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/cards"
	"github.com/onuigboprecious/infarbloom/backend/internal/db"
	"github.com/onuigboprecious/infarbloom/backend/internal/leads"
	"github.com/onuigboprecious/infarbloom/backend/internal/links"
	"github.com/onuigboprecious/infarbloom/backend/internal/middleware"
	"github.com/onuigboprecious/infarbloom/backend/internal/profiles"
	"github.com/onuigboprecious/infarbloom/backend/internal/socials"
	"github.com/onuigboprecious/infarbloom/backend/internal/store"
)

// loadEnv reads a .env file and populates environment variables if not already defined.
func loadEnv(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Warning: error reading %s: %v", filepath, err)
	}
}

func main() {
	// Auto-load local .env file if present
	loadEnv(".env")
	loadEnv(".env.local")

	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	dbURL := os.Getenv("DATABASE_URL")
	var database *sql.DB
	if dbURL != "" {
		var err error
		database, err = db.InitDB(dbURL)
		if err != nil {
			log.Printf("Warning: PostgreSQL initialization issue: %v", err)
		} else {
			log.Println("PostgreSQL connected successfully!")
		}
	} else {
		log.Println("Notice: DATABASE_URL is not set. Running with fallback in-memory store.")
	}
	if database != nil {
		defer database.Close()
	}

	authSvc := auth.New(database, env)
	leadsSvc := leads.New(database)
	profilesSvc := profiles.NewService(database)
	profilesHandler := profiles.NewHandler(profilesSvc, authSvc)
	cardsSvc := cards.NewService(database)
	cardsHandler := cards.NewHandler(cardsSvc, authSvc)
	socialsSvc := socials.NewService(database)
	socialsHandler := socials.NewHandler(socialsSvc)
	linksSvc := links.NewService(database)
	linksHandler := links.NewHandler(linksSvc)
	analyticsSvc := analytics.New(database)
	storeSvc := store.New(database)

	mux := http.NewServeMux()

	// Health Check & Root
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","message":"Enlazar NFC Backend API Server is live"}`))
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","environment":"` + env + `"}`))
	})

	// 1. Authentication Endpoints (Support both Bloom and Enlazar specs)
	mux.HandleFunc("POST /api/signup", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/login", authSvc.HandleLogin)
	mux.HandleFunc("POST /api/logout", authSvc.HandleLogout)
	mux.HandleFunc("GET /api/me", authSvc.HandleMe)
	mux.HandleFunc("POST /api/forgot-password", authSvc.HandleForgotPassword)
	mux.HandleFunc("POST /api/reset-password", authSvc.HandleResetPassword)

	mux.HandleFunc("POST /api/auth/register", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/auth/signup", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/auth/login", authSvc.HandleLogin)
	mux.HandleFunc("POST /api/auth/logout", authSvc.HandleLogout)
	mux.HandleFunc("GET /api/auth/me", authSvc.HandleMe)
	mux.HandleFunc("POST /api/auth/forgot-password", authSvc.HandleForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", authSvc.HandleResetPassword)

	// Google OAuth & ID Token Endpoints
	mux.HandleFunc("POST /api/auth/google", authSvc.HandleGoogleAuth)
	mux.HandleFunc("POST /api/google/login", authSvc.HandleGoogleAuth)
	mux.HandleFunc("GET /api/auth/google/login", authSvc.HandleGoogleOAuthLogin)
	mux.HandleFunc("GET /api/auth/google/callback", authSvc.HandleGoogleOAuthCallback)

	// 2. Profiles & Studio Management
	mux.HandleFunc("GET /api/profile/check-handle", profilesHandler.HandleCheckHandle)
	mux.HandleFunc("GET /api/profile/me", profilesHandler.HandleGetMyProfile)
	mux.HandleFunc("GET /api/profile", profilesHandler.HandleGetMyProfile)
	mux.HandleFunc("PUT /api/profile/me", profilesHandler.HandleUpdateMyProfile)
	mux.HandleFunc("PUT /api/profile", profilesHandler.HandleUpdateMyProfile)

	// Enlazer Public Profile routes
	mux.HandleFunc("GET /api/profile/public/{username}", profilesHandler.HandleGetProfile)
	mux.HandleFunc("GET /api/profile/public/", profilesHandler.HandleGetProfile)
	mux.HandleFunc("GET /api/profile/", profilesHandler.HandleGetProfile)
	mux.HandleFunc("GET /api/profile/{username}", profilesHandler.HandleGetProfile)

	// 3. Dynamic Social Handles (/api/socials)
	mux.HandleFunc("GET /api/socials", socialsHandler.HandleGetSocials)
	mux.HandleFunc("POST /api/socials", socialsHandler.HandleSyncSocials)
	mux.HandleFunc("DELETE /api/socials/{id}", socialsHandler.HandleDeleteSocial)

	// 4. Custom Bio Linktree Buttons (/api/custom-links)
	mux.HandleFunc("GET /api/custom-links", linksHandler.HandleGetLinks)
	mux.HandleFunc("POST /api/custom-links", linksHandler.HandleCreateLink)
	mux.HandleFunc("DELETE /api/custom-links/{id}", linksHandler.HandleDeleteLink)

	// 5. Hardware Cards & Claiming
	mux.HandleFunc("POST /api/cards/claim", cardsHandler.HandleClaimCard)
	mux.HandleFunc("POST /api/cards/activate", cardsHandler.HandleActivateCard)
	mux.HandleFunc("GET /api/cards/me", cardsHandler.HandleGetMyCards)

	// Admin Tag CRUD Endpoints
	mux.HandleFunc("POST /api/admin/cards", cardsHandler.HandleCreateCard)
	mux.HandleFunc("GET /api/admin/cards", cardsHandler.HandleListCards)
	mux.HandleFunc("PUT /api/admin/cards/{cardUid}", cardsHandler.HandleUpdateCard)
	mux.HandleFunc("DELETE /api/admin/cards/{cardUid}", cardsHandler.HandleDeleteCard)
	mux.HandleFunc("POST /api/admin/cards/provision", cardsHandler.HandleBatchProvision)

	// vCard downloads
	mux.HandleFunc("GET /api/vcard/", profilesHandler.HandleGetVCard)
	mux.HandleFunc("GET /api/vcard/{username}", profilesHandler.HandleGetVCard)

	// 6. Lead Capture Endpoints
	mux.HandleFunc("POST /api/leads", leadsSvc.HandleCreateLead)
	mux.HandleFunc("GET /api/leads", leadsSvc.HandleGetLeads)
	mux.HandleFunc("DELETE /api/leads/{id}", leadsSvc.HandleDeleteLead)

	// 7. Analytics & Tap Tracking
	mux.HandleFunc("POST /api/taps/record", analyticsSvc.HandleRecordTap)
	mux.HandleFunc("POST /api/analytics/tap", analyticsSvc.HandleRecordTap)
	mux.HandleFunc("GET /api/analytics", analyticsSvc.HandleGetAnalytics)

	// 8. VIP Waitlist & Orders & Paystack Payment Stack
	mux.HandleFunc("POST /api/waitlist", storeSvc.HandleWaitlist)
	mux.HandleFunc("POST /api/orders", storeSvc.HandleOrders)
	mux.HandleFunc("GET /api/orders", storeSvc.HandleListOrders)
	mux.HandleFunc("GET /api/admin/orders", storeSvc.HandleListOrders)
	mux.HandleFunc("PATCH /api/admin/orders/{id}/status", storeSvc.HandleUpdateOrderStatus)
	mux.HandleFunc("PUT /api/admin/orders/{id}/status", storeSvc.HandleUpdateOrderStatus)
	mux.HandleFunc("POST /api/paystack/initialize", storeSvc.HandleInitializePaystack)
	mux.HandleFunc("GET /api/paystack/verify/{reference}", storeSvc.HandleVerifyPaystack)
	mux.HandleFunc("GET /api/paystack/verify", storeSvc.HandleVerifyPaystack)
	mux.HandleFunc("POST /api/paystack/webhook", storeSvc.HandlePaystackWebhook)
	mux.HandleFunc("POST /webhooks/paystack", storeSvc.HandlePaystackWebhook)

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	handler := middleware.CORS(frontendOrigin, mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Enlazar API listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
