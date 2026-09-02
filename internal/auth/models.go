package auth

import "time"

// User represents a system user with hashed credentials.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	GoogleID     string    `json:"google_id,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	IsPro        bool      `json:"is_pro"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// UserPublic represents the safe public view of a user matching the spec payload.
type UserPublic struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	GoogleID  string    `json:"google_id,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Session represents an active authenticated user session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ToPublic converts a User to a UserPublic struct.
func (u *User) ToPublic() UserPublic {
	return UserPublic{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Username:  u.Username,
		AvatarURL: u.AvatarURL,
		GoogleID:  u.GoogleID,
		CreatedAt: u.CreatedAt,
	}
}
