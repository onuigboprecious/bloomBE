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

// BloomProfile matches the frontend specification for GET /api/profile/@:username
type BloomProfile struct {
	Name     string                 `json:"name"`
	Username string                 `json:"username"`
	Title    string                 `json:"title"`
	Company  string                 `json:"company"`
	Bio      string                 `json:"bio"`
	Avatar   string                 `json:"avatar"`
	Email    string                 `json:"email"`
	Phone    string                 `json:"phone"`
	Website  string                 `json:"website"`
	Location string                 `json:"location"`
	Theme    string                 `json:"theme"`
	Layout   string                 `json:"layout"`
	CardUid  string                 `json:"cardUid"`
	Socials  map[string]interface{} `json:"socials"`
	Stats    ProfileStats           `json:"stats"`
}

// UpdateBloomProfileRequest is the payload for PUT /api/profile/me
type UpdateBloomProfileRequest struct {
	Name     *string                `json:"name,omitempty"`
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

// RecordTapRequest is the payload for POST /api/taps/record
type RecordTapRequest struct {
	CardUid string `json:"cardUid"`
	Method  string `json:"method"`
}

// HourlyTap represents hourly tap analytics distribution
type HourlyTap struct {
	Hour string `json:"hour"`
	Taps int    `json:"taps"`
}

// AnalyticsResponse matches GET /api/analytics response
type AnalyticsResponse struct {
	TotalTaps      int         `json:"totalTaps"`
	MonthlyTaps    int         `json:"monthlyTaps"`
	UniqueVisitors int         `json:"uniqueVisitors"`
	LeadsCaptured  int         `json:"leadsCaptured"`
	ConversionRate int         `json:"conversionRate"`
	HourlyTaps     []HourlyTap `json:"hourlyTaps"`
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
