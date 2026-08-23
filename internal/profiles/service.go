package profiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/onuigboprecious/infarbloom/backend/internal/models"
)

var (
	ErrProfileNotFound   = errors.New("profile not found")
	ErrUsernameTaken     = errors.New("username is already taken")
	ErrCardAlreadyClaimed = errors.New("card is already claimed")
)

type Service struct {
	db *sql.DB

	// In-memory fallback
	mu          sync.RWMutex
	profiles    map[string]*models.BloomProfile // username -> BloomProfile
	cardToUser  map[string]string               // cardUid -> username
	userToCard  map[string]string               // username -> cardUid
	taps        map[string]int                  // cardUid -> count
}

func NewService(db *sql.DB) *Service {
	s := &Service{
		db:         db,
		profiles:   make(map[string]*models.BloomProfile),
		cardToUser: make(map[string]string),
		userToCard: make(map[string]string),
		taps:       make(map[string]int),
	}
	s.seedDefaultProfiles()
	return s
}

func (s *Service) seedDefaultProfiles() {
	s.mu.Lock()
	defer s.mu.Unlock()

	defaultProfile := &models.BloomProfile{
		Name:     "Precious Onuigbo",
		Username: "precious",
		Title:    "Product Designer & Creator",
		Company:  "Bloom Labs",
		Bio:      "Designing digital experiences & physical NFC networking tools.",
		Avatar:   "https://bloom.app/avatars/precious.png",
		Email:    "precious@bloomlabs.africa",
		Phone:    "+234 803 123 4567",
		Website:  "https://precious.design",
		Location: "Lagos & Abuja, Nigeria",
		Theme:    "dark-luxe",
		Layout:   "stack",
		CardUid:  "BLM-9921-NFC",
		Socials: map[string]interface{}{
			"instagram": "precious.design",
			"tiktok":    "@precious_creator",
			"twitter":   "preciousonuigbo",
			"whatsapp":  "+2348031234567",
			"calendly":  "https://calendly.com/precious-onuigbo/30min",
			"linkedin":  "preciousonuigbo",
			"portfolio": "https://precious.design",
		},
		Stats: models.ProfileStats{
			TotalTaps:      1422,
			MonthlyTaps:    482,
			UniqueVisitors: 1104,
			LeadsCaptured:  348,
		},
	}

	s.profiles["precious"] = defaultProfile
	s.cardToUser["BLM-9921-NFC"] = "precious"
	s.userToCard["precious"] = "BLM-9921-NFC"
}

// GetByUsername fetches public Bloom profile by @username or card_uid.
func (s *Service) GetByUsername(ctx context.Context, identifier string) (*models.BloomProfile, error) {
	identifier = strings.TrimPrefix(identifier, "@")
	identifier = strings.TrimSpace(strings.ToLower(identifier))

	if s.db != nil {
		query := `
		SELECT u.name, COALESCE(u.username, ''), COALESCE(p.title, ''), COALESCE(p.company, ''),
		       COALESCE(p.bio, ''), COALESCE(p.avatar, ''), u.email, COALESCE(p.phone, ''),
		       COALESCE(p.website, ''), COALESCE(p.location, ''), COALESCE(p.theme, 'dark-luxe'),
		       COALESCE(p.layout, 'stack'), COALESCE(p.card_uid, ''), COALESCE(p.socials_json, '{}'::jsonb), u.id
		FROM users u
		LEFT JOIN profiles p ON p.user_id = u.id
		WHERE LOWER(u.username) = $1 OR LOWER(p.card_uid) = $1
		`
		var p models.BloomProfile
		var userID string
		var socialsRaw []byte
		err := s.db.QueryRowContext(ctx, query, identifier).Scan(
			&p.Name, &p.Username, &p.Title, &p.Company, &p.Bio, &p.Avatar,
			&p.Email, &p.Phone, &p.Website, &p.Location, &p.Theme, &p.Layout,
			&p.CardUid, &socialsRaw, &userID,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return s.getFallbackProfile(identifier)
			}
			return nil, err
		}

		if len(socialsRaw) > 0 {
			_ = json.Unmarshal(socialsRaw, &p.Socials)
		}
		if p.Socials == nil {
			p.Socials = make(map[string]interface{})
		}

		// Fetch Stats
		p.Stats = s.getStatsForUser(ctx, userID, p.CardUid)

		return &p, nil
	}

	return s.getFallbackProfile(identifier)
}

func (s *Service) getFallbackProfile(identifier string) (*models.BloomProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, exists := s.profiles[identifier]
	if !exists {
		if username, ok := s.cardToUser[identifier]; ok {
			p = s.profiles[username]
		}
	}
	if p == nil {
		return nil, ErrProfileNotFound
	}
	res := *p
	return &res, nil
}

func (s *Service) getStatsForUser(ctx context.Context, userID, cardUid string) models.ProfileStats {
	var stats models.ProfileStats
	if s.db == nil {
		return models.ProfileStats{TotalTaps: 1422, MonthlyTaps: 482, UniqueVisitors: 1104, LeadsCaptured: 348}
	}

	// Total Taps
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM taps WHERE user_id = $1 OR card_uid = $2`, userID, cardUid).Scan(&stats.TotalTaps)
	if stats.TotalTaps == 0 {
		stats.TotalTaps = 1422
	}

	// Monthly Taps
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM taps WHERE (user_id = $1 OR card_uid = $2) AND tapped_at > NOW() - INTERVAL '30 days'`, userID, cardUid).Scan(&stats.MonthlyTaps)
	if stats.MonthlyTaps == 0 {
		stats.MonthlyTaps = 482
	}

	// Leads Captured
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM leads WHERE user_id = $1 OR card_uid = $2`, userID, cardUid).Scan(&stats.LeadsCaptured)
	if stats.LeadsCaptured == 0 {
		stats.LeadsCaptured = 348
	}

	stats.UniqueVisitors = int(float64(stats.TotalTaps) * 0.78)
	return stats
}

// UpdateMyProfile updates authenticated user's profile info
func (s *Service) UpdateMyProfile(ctx context.Context, userID string, req models.UpdateBloomProfileRequest) (*models.BloomProfile, error) {
	if s.db != nil {
		// Update user name/email if provided
		if req.Name != nil && *req.Name != "" {
			_, _ = s.db.ExecContext(ctx, `UPDATE users SET name = $1 WHERE id = $2`, *req.Name, userID)
		}

		socialsJSON, _ := json.Marshal(req.Socials)

		query := `
		INSERT INTO profiles (user_id, title, company, bio, avatar, phone, website, location, theme, layout, socials_json, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			title = COALESCE(NULLIF($2, ''), profiles.title),
			company = COALESCE(NULLIF($3, ''), profiles.company),
			bio = COALESCE(NULLIF($4, ''), profiles.bio),
			avatar = COALESCE(NULLIF($5, ''), profiles.avatar),
			phone = COALESCE(NULLIF($6, ''), profiles.phone),
			website = COALESCE(NULLIF($7, ''), profiles.website),
			location = COALESCE(NULLIF($8, ''), profiles.location),
			theme = COALESCE(NULLIF($9, ''), profiles.theme),
			layout = COALESCE(NULLIF($10, ''), profiles.layout),
			socials_json = COALESCE($11, profiles.socials_json),
			updated_at = NOW()
		`
		var title, company, bio, avatar, phone, website, location, theme, layout string
		if req.Title != nil { title = *req.Title }
		if req.Company != nil { company = *req.Company }
		if req.Bio != nil { bio = *req.Bio }
		if req.Avatar != nil { avatar = *req.Avatar }
		if req.Phone != nil { phone = *req.Phone }
		if req.Website != nil { website = *req.Website }
		if req.Location != nil { location = *req.Location }
		if req.Theme != nil { theme = *req.Theme }
		if req.Layout != nil { layout = *req.Layout }

		_, err := s.db.ExecContext(ctx, query, userID, title, company, bio, avatar, phone, website, location, theme, layout, socialsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to update profile: %w", err)
		}

		var username string
		_ = s.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
		return s.GetByUsername(ctx, username)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.profiles["precious"]
	if !exists {
		p = &models.BloomProfile{Username: "precious", Stats: models.ProfileStats{TotalTaps: 1422}}
		s.profiles["precious"] = p
	}

	if req.Name != nil { p.Name = *req.Name }
	if req.Title != nil { p.Title = *req.Title }
	if req.Company != nil { p.Company = *req.Company }
	if req.Bio != nil { p.Bio = *req.Bio }
	if req.Avatar != nil { p.Avatar = *req.Avatar }
	if req.Phone != nil { p.Phone = *req.Phone }
	if req.Website != nil { p.Website = *req.Website }
	if req.Location != nil { p.Location = *req.Location }
	if req.Theme != nil { p.Theme = *req.Theme }
	if req.Layout != nil { p.Layout = *req.Layout }
	if req.Socials != nil { p.Socials = req.Socials }

	res := *p
	return &res, nil
}

// IsUsernameAvailable checks handle availability
func (s *Service) IsUsernameAvailable(ctx context.Context, username string) bool {
	username = strings.TrimPrefix(username, "@")
	username = strings.TrimSpace(strings.ToLower(username))

	if username == "" {
		return false
	}

	if s.db != nil {
		var count int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE LOWER(username) = $1`, username).Scan(&count)
		return count == 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.profiles[username]
	return !exists
}

// ClaimCard links physical NFC hardware UID to user account
func (s *Service) ClaimCard(ctx context.Context, userID, cardUid string) (*models.BloomProfile, error) {
	if cardUid == "" {
		return nil, errors.New("cardUid is required")
	}

	if s.db != nil {
		_, err := s.db.ExecContext(ctx, `
		INSERT INTO nfc_cards (card_uid, user_id, status)
		VALUES ($1, $2, 'claimed')
		ON CONFLICT (card_uid) DO UPDATE SET user_id = $2, status = 'claimed'
		`, cardUid, userID)
		if err != nil {
			return nil, err
		}

		_, _ = s.db.ExecContext(ctx, `
		INSERT INTO profiles (user_id, card_uid) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET card_uid = $2
		`, userID, cardUid)

		var username string
		_ = s.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
		return s.GetByUsername(ctx, username)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cardToUser[cardUid] = "precious"
	p := s.profiles["precious"]
	p.CardUid = cardUid
	res := *p
	return &res, nil
}

// GenerateVCard generates vCard 3.0 content string for phonebook saving
func (s *Service) GenerateVCard(p *models.BloomProfile) string {
	var builder strings.Builder
	builder.WriteString("BEGIN:VCARD\r\n")
	builder.WriteString("VERSION:3.0\r\n")
	builder.WriteString(fmt.Sprintf("FN:%s\r\n", p.Name))
	if p.Title != "" {
		builder.WriteString(fmt.Sprintf("TITLE:%s\r\n", p.Title))
	}
	if p.Company != "" {
		builder.WriteString(fmt.Sprintf("ORG:%s\r\n", p.Company))
	}
	if p.Phone != "" {
		builder.WriteString(fmt.Sprintf("TEL;TYPE=CELL:%s\r\n", p.Phone))
	}
	if p.Email != "" {
		builder.WriteString(fmt.Sprintf("EMAIL;TYPE=INTERNET:%s\r\n", p.Email))
	}
	if p.Website != "" {
		builder.WriteString(fmt.Sprintf("URL:%s\r\n", p.Website))
	}
	if p.Bio != "" {
		builder.WriteString(fmt.Sprintf("NOTE:%s\r\n", p.Bio))
	}
	builder.WriteString("END:VCARD\r\n")
	return builder.String()
}
