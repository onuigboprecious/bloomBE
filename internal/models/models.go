package models

import "time"

// Status constants for profiles & cards
const (
	StatusProvisioned = "provisioned"
	StatusClaimed     = "claimed"
	StatusActive      = "active"
)

// ProfileStats holds analytics summary counters for a public profile.
type ProfileStats struct {
	TotalTaps      int `json:"totalTaps"`
	MonthlyTaps    int `json:"monthlyTaps"`
	UniqueVisitors int `json:"uniqueVisitors"`
	LeadsCaptured  int `json:"leadsCaptured"`
}

// SocialHandle represents a connected social media handle (Prisma/Enlazer specification)
type SocialHandle struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Platform  string    `json:"platform"`
	Handle    string    `json:"handle"`
	CreatedAt time.Time `json:"createdAt"`
}

// CustomLink represents a custom bio link button (Prisma/Enlazer specification)
type CustomLink struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Label     string    `json:"label"`
	URL       string    `json:"url"`
	Order     int       `json:"order"`
	CreatedAt time.Time `json:"createdAt"`
}

// TapAnalytic represents a detailed geolocation & device tap log entry
type TapAnalytic struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	CardUid   string    `json:"cardUid,omitempty"`
	DeviceOS  string    `json:"deviceOs"`
	Location  string    `json:"location"`
	IPAddress string    `json:"ipAddress"`
	UserAgent string    `json:"userAgent"`
	Timestamp time.Time `json:"timestamp"`
}

// BloomProfile matches the frontend specification for GET /api/profile/@:username and GET /api/profile/public/:username
type BloomProfile struct {
	Name        string                 `json:"name"`
	Username    string                 `json:"username"`
	Title       string                 `json:"title"`
	Company     string                 `json:"company"`
	Bio         string                 `json:"bio"`
	Avatar      string                 `json:"avatar"`
	Email       string                 `json:"email"`
	Phone       string                 `json:"phone"`
	Website     string                 `json:"website"`
	Location    string                 `json:"location"`
	Theme       string                 `json:"theme"`
	Layout      string                 `json:"layout"`
	CardUid     string                 `json:"cardUid"`
	Socials     map[string]interface{} `json:"socials"`
	SocialList  []SocialHandle         `json:"socialHandles,omitempty"`
	CustomLinks []CustomLink           `json:"customLinks,omitempty"`
	Stats       ProfileStats           `json:"stats"`
}

// UpdateBloomProfileRequest is the payload for PUT /api/profile/me and PUT /api/profile
type UpdateBloomProfileRequest struct {
	Name     *string                `json:"name,omitempty"`
	Username *string                `json:"username,omitempty"`
	Title    *string                `json:"title,omitempty"`
	Company  *string                `json:"company,omitempty"`
	Bio      *string                `json:"bio,omitempty"`
	Avatar   *string                `json:"avatar,omitempty"`
	Email    *string                `json:"email,omitempty"`
	Phone    *string                `json:"phone,omitempty"`
	Website  *string                `json:"website,omitempty"`
	Location *string                `json:"location,omitempty"`
	Theme    *string                `json:"theme,omitempty"`
	Layout   *string                `json:"layout,omitempty"`
	Socials  map[string]interface{} `json:"socials,omitempty"`
}

// BulkSocialsPayload is the payload for POST /api/socials
type BulkSocialsPayload struct {
	Socials []struct {
		Platform string `json:"platform"`
		Handle   string `json:"handle"`
	} `json:"socials"`
	Platform string `json:"platform,omitempty"`
	Handle   string `json:"handle,omitempty"`
}

// CreateCustomLinkPayload is the payload for POST /api/custom-links
type CreateCustomLinkPayload struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Order int    `json:"order,omitempty"`
}

// ActivateCardPayload is the payload for POST /api/cards/activate
type ActivateCardPayload struct {
	CardUid  string `json:"cardUid"`
	ChipType string `json:"chipType,omitempty"`
}

// ClaimCardRequest is the payload for POST /api/cards/claim
type ClaimCardRequest struct {
	CardUid string `json:"cardUid"`
}

// Lead represents a lead captured via the "Share Back" form
type Lead struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId,omitempty"`
	CardUid   string    `json:"cardUid"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Role      string    `json:"role"`
	Method    string    `json:"method"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"createdAt"`
}

// RecordTapRequest is the payload for POST /api/taps/record and POST /api/analytics/tap
type RecordTapRequest struct {
	CardUid   string `json:"cardUid"`
	DeviceOS  string `json:"deviceOs,omitempty"`
	Location  string `json:"location,omitempty"`
	IPAddress string `json:"ipAddress,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
	Method    string `json:"method,omitempty"`
}

// HourlyTap represents hourly tap analytics distribution
type HourlyTap struct {
	Hour string `json:"hour"`
	Taps int    `json:"taps"`
}

// DeviceOSBreakdown represents percentage breakdown by device OS
type DeviceOSBreakdown struct {
	OS         string `json:"os"`
	Percentage int    `json:"percentage"`
}

// AnalyticsResponse matches GET /api/analytics response
type AnalyticsResponse struct {
	TotalTaps      int                 `json:"totalTaps"`
	MonthlyTaps    int                 `json:"monthlyTaps"`
	UniqueVisitors int                 `json:"uniqueVisitors"`
	LeadsCaptured  int                 `json:"leadsCaptured"`
	ConversionRate int                 `json:"conversionRate"`
	HourlyTaps     []HourlyTap         `json:"hourlyTaps"`
	DeviceOS       []DeviceOSBreakdown `json:"deviceOs,omitempty"`
}

// WaitlistRequest is the payload for POST /api/waitlist
type WaitlistRequest struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Phone           string `json:"phone"`
	PreferredFinish string `json:"preferredFinish"`
}

// OrderRequest is the payload for POST /api/orders
type OrderRequest struct {
	FinishID        string `json:"finishId"`
	FinishName      string `json:"finishName"`
	Quantity        int    `json:"quantity"`
	Amount          int    `json:"amount"`
	DeliveryAddress string `json:"deliveryAddress"`
}
