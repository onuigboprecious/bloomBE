package analytics

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/onuigboprecious/infarbloom/backend/internal/auth"
	"github.com/onuigboprecious/infarbloom/backend/internal/models"
)

type Service struct {
	db        *sql.DB
	mu        sync.RWMutex
	totalTaps int
}

func New(db *sql.DB) *Service {
	return &Service{
		db:        db,
		totalTaps: 1422,
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// HandleRecordTap handles POST /api/taps/record and POST /api/analytics/tap
func (s *Service) HandleRecordTap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.RecordTapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	if req.CardUid == "" {
		req.CardUid = "ENZ-9921-NFC"
	}
	if req.Method == "" {
		req.Method = "NFC Tap"
	}
	if req.DeviceOS == "" {
		req.DeviceOS = "iOS"
	}
	if req.Location == "" {
		req.Location = "Lagos, Nigeria"
	}

	if s.db != nil {
		cardUid := strings.TrimSpace(req.CardUid)
		var actualCardUid string
		var userID string
		var uNull sql.NullString

		// 1. Check nfc_cards
		err := s.db.QueryRowContext(r.Context(), `SELECT card_uid, user_id FROM nfc_cards WHERE LOWER(card_uid) = LOWER($1)`, cardUid).Scan(&actualCardUid, &uNull)
		if err == nil {
			if uNull.Valid {
				userID = uNull.String
			}
		} else {
			// 2. Check profiles
			err = s.db.QueryRowContext(r.Context(), `SELECT card_uid, user_id FROM profiles WHERE LOWER(card_uid) = LOWER($1) OR LOWER(user_id::text) = LOWER($1)`, cardUid).Scan(&actualCardUid, &userID)
			if err != nil {
				// 3. Check users table by username or email
				_ = s.db.QueryRowContext(r.Context(), `
					SELECT COALESCE(p.card_uid, ''), u.id 
					FROM users u 
					LEFT JOIN profiles p ON p.user_id = u.id 
					WHERE LOWER(u.username) = LOWER($1) OR LOWER(u.email) = LOWER($1)
				`, cardUid).Scan(&actualCardUid, &userID)
			}
		}

		if actualCardUid == "" {
			actualCardUid = cardUid
		}

		var uVal interface{} = nil
		if userID != "" {
			uVal = userID
		}

		_, err = s.db.ExecContext(r.Context(), `INSERT INTO taps (card_uid, user_id, method, tapped_at) VALUES ($1, $2, $3, NOW())`, actualCardUid, uVal, req.Method)
		if err != nil {
			log.Printf("analytics: warning tap record error: %v", err)
		}

		if userID != "" {
			_, _ = s.db.ExecContext(r.Context(),
				`INSERT INTO tap_analytics (user_id, card_uid, device_os, location, ip_address, user_agent, timestamp) VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
				userID, actualCardUid, req.DeviceOS, req.Location, req.IPAddress, req.UserAgent,
			)
		}

		// Update taps_count on nfc_cards case-insensitively
		res, err := s.db.ExecContext(r.Context(), `UPDATE nfc_cards SET taps_count = taps_count + 1 WHERE LOWER(card_uid) = LOWER($1)`, actualCardUid)
		if err == nil {
			if rows, _ := res.RowsAffected(); rows == 0 {
				// Insert if card row doesn't exist yet
				_, _ = s.db.ExecContext(r.Context(), `
					INSERT INTO nfc_cards (card_uid, user_id, finish_name, status, taps_count, created_at)
					VALUES ($1, $2, 'NFC Card', 'claimed', 1, NOW())
					ON CONFLICT (card_uid) DO UPDATE SET taps_count = nfc_cards.taps_count + 1
				`, actualCardUid, uVal)
			}
		}
	}

	s.mu.Lock()
	s.totalTaps++
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "success",
		"message":  "Tap recorded successfully",
		"cardUid":  req.CardUid,
		"deviceOs": req.DeviceOS,
		"location": req.Location,
	})
}

// HandleGetAnalytics handles GET /api/analytics (Protected)
func (s *Service) HandleGetAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, _ := auth.CurrentUserFromContext(r)

	timeframe := strings.TrimSpace(r.URL.Query().Get("timeframe"))
	var intervalSql string
	switch timeframe {
	case "24h", "24 hours", "1d":
		timeframe = "24h"
		intervalSql = "24 hours"
	case "7d", "7 days":
		timeframe = "7d"
		intervalSql = "7 days"
	default:
		timeframe = "30d"
		intervalSql = "30 days"
	}

	total := 0
	periodTaps := 0
	leads := 0
	uniqueVisitors := 0
	deviceOS := []models.DeviceOSBreakdown{}
	locations := []models.LocationBreakdown{}

	if s.db != nil && user != nil {
		// 1. Lifetime Total Taps
		_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM taps WHERE user_id = $1`, user.ID).Scan(&total)

		// 2. Period Taps based on selected timeframe
		_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM taps WHERE user_id = $1 AND tapped_at >= NOW() - $2::interval`, user.ID, intervalSql).Scan(&periodTaps)

		// 3. Leads Captured within timeframe
		_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM leads WHERE user_id = $1 AND created_at >= NOW() - $2::interval`, user.ID, intervalSql).Scan(&leads)

		// 4. Unique Visitors within timeframe from tap_analytics
		_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT ip_address) FROM tap_analytics WHERE user_id = $1 AND timestamp >= NOW() - $2::interval`, user.ID, intervalSql).Scan(&uniqueVisitors)

		// 5. Device OS distribution
		rows, err := s.db.QueryContext(r.Context(), `
			SELECT device_os, COUNT(*) 
			FROM tap_analytics 
			WHERE user_id = $1 AND timestamp >= NOW() - $2::interval 
			GROUP BY device_os
		`, user.ID, intervalSql)
		if err == nil {
			var totalOsTaps int
			type osCount struct {
				os  string
				cnt int
			}
			var list []osCount
			for rows.Next() {
				var osName string
				var cnt int
				if err := rows.Scan(&osName, &cnt); err == nil && osName != "" {
					list = append(list, osCount{os: osName, cnt: cnt})
					totalOsTaps += cnt
				}
			}
			_ = rows.Err()
			rows.Close()
			for _, item := range list {
				pct := 0
				if totalOsTaps > 0 {
					pct = int((float64(item.cnt) / float64(totalOsTaps)) * 100)
				}
				deviceOS = append(deviceOS, models.DeviceOSBreakdown{
					OS:         item.os,
					Percentage: pct,
					Taps:       item.cnt,
				})
			}
		}

		// 6. Geographic Location distribution
		locRows, err := s.db.QueryContext(r.Context(), `
			SELECT location, COUNT(*) 
			FROM tap_analytics 
			WHERE user_id = $1 AND timestamp >= NOW() - $2::interval 
			GROUP BY location
		`, user.ID, intervalSql)
		if err == nil {
			var totalLocTaps int
			type locCount struct {
				loc string
				cnt int
			}
			var list []locCount
			for locRows.Next() {
				var locName string
				var cnt int
				if err := locRows.Scan(&locName, &cnt); err == nil && locName != "" {
					list = append(list, locCount{loc: locName, cnt: cnt})
					totalLocTaps += cnt
				}
			}
			_ = locRows.Err()
			locRows.Close()
			for _, item := range list {
				pct := 0
				if totalLocTaps > 0 {
					pct = int((float64(item.cnt) / float64(totalLocTaps)) * 100)
				}
				locations = append(locations, models.LocationBreakdown{
					Location:   item.loc,
					Percentage: pct,
					Taps:       item.cnt,
				})
			}
		}
	}

	if periodTaps > 0 && uniqueVisitors == 0 {
		uniqueVisitors = periodTaps
	}

	conversionRate := 0
	if uniqueVisitors > 0 && leads > 0 {
		conversionRate = int((float64(leads) / float64(uniqueVisitors)) * 100)
		if conversionRate > 100 {
			conversionRate = 100
		}
	}

	resp := models.AnalyticsResponse{
		Timeframe:      timeframe,
		TotalTaps:      total,
		MonthlyTaps:    periodTaps,
		UniqueVisitors: uniqueVisitors,
		LeadsCaptured:  leads,
		ConversionRate: conversionRate,
		HourlyTaps:     []models.HourlyTap{},
		DeviceOS:       deviceOS,
		Locations:      locations,
	}

	writeJSON(w, http.StatusOK, resp)
}
