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
	"github.com/onuigboprecious/infarbloom/backend/internal/db"
	"github.com/onuigboprecious/infarbloom/backend/internal/leads"
	"github.com/onuigboprecious/infarbloom/backend/internal/middleware"
	"github.com/onuigboprecious/infarbloom/backend/internal/profiles"
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
	analyticsSvc := analytics.New(database)
	storeSvc := store.New(database)

	mux := http.NewServeMux()

	// Health Check & Root
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","message":"Bloom NFC Backend API Server is live"}`))
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","environment":"` + env + `"}`))
	})

	// 1. Authentication Endpoints
	mux.HandleFunc("POST /api/signup", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/login", authSvc.HandleLogin)
	mux.HandleFunc("POST /api/logout", authSvc.HandleLogout)
	mux.HandleFunc("GET /api/me", authSvc.HandleMe)
	mux.HandleFunc("POST /api/forgot-password", authSvc.HandleForgotPassword)
	mux.HandleFunc("POST /api/reset-password", authSvc.HandleResetPassword)

	mux.HandleFunc("POST /api/auth/signup", authSvc.HandleSignup)
	mux.HandleFunc("POST /api/auth/login", authSvc.HandleLogin)
	mux.HandleFunc("POST /api/auth/logout", authSvc.HandleLogout)
	mux.HandleFunc("GET /api/auth/me", authSvc.HandleMe)
	mux.HandleFunc("POST /api/auth/forgot-password", authSvc.HandleForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", authSvc.HandleResetPassword)

	// 2. Profiles & Card Management
	mux.HandleFunc("GET /api/profile/check-handle", profilesHandler.HandleCheckHandle)
	mux.HandleFunc("PUT /api/profile/me", profilesHandler.HandleUpdateMyProfile)
	mux.HandleFunc("POST /api/cards/claim", profilesHandler.HandleClaimCard)

	mux.HandleFunc("GET /api/profile/", profilesHandler.HandleGetProfile)
	mux.HandleFunc("GET /api/profile/{username}", profilesHandler.HandleGetProfile)

	mux.HandleFunc("GET /api/vcard/", profilesHandler.HandleGetVCard)
	mux.HandleFunc("GET /api/vcard/{username}", profilesHandler.HandleGetVCard)

	// 3. Lead Capture Endpoints
	mux.HandleFunc("POST /api/leads", leadsSvc.HandleCreateLead)
	mux.HandleFunc("GET /api/leads", leadsSvc.HandleGetLeads)
	mux.HandleFunc("DELETE /api/leads/{id}", leadsSvc.HandleDeleteLead)

	// 4. Analytics & Tap Tracking
	mux.HandleFunc("POST /api/taps/record", analyticsSvc.HandleRecordTap)
	mux.HandleFunc("GET /api/analytics", analyticsSvc.HandleGetAnalytics)

	// 5. VIP Waitlist & Orders
	mux.HandleFunc("POST /api/waitlist", storeSvc.HandleWaitlist)
	mux.HandleFunc("POST /api/orders", storeSvc.HandleOrders)

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	handler := middleware.CORS(frontendOrigin, mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Bloom API listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
