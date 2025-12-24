package models

import "time"

// ShareLink represents a shareable link to a wiki page
type ShareLink struct {
	ID              int64
	TokenHash       string // SHA-256 hash of the token (never store raw token)
	PageID          int64
	CreatedBy       int64
	IncludeChildren bool
	MaxViews        *int       // nil = unlimited
	MaxIPs          *int       // nil = unlimited
	ExpiresAt       *time.Time // nil = never expires
	IsRevoked       bool
	ViewCount       int
	CreatedAt       time.Time

	// Joined fields for display
	PageTitle       string
	PageSlug        string
	CreatorUsername string
	UniqueIPs       int // Count of unique IPs that accessed this link
}

// IsValid checks if the share link is currently valid for access
func (s *ShareLink) IsValid() bool {
	if s.IsRevoked {
		return false
	}
	if s.ExpiresAt != nil && time.Now().After(*s.ExpiresAt) {
		return false
	}
	if s.MaxViews != nil && s.ViewCount >= *s.MaxViews {
		return false
	}
	return true
}

// IsExpired checks if the link has expired
func (s *ShareLink) IsExpired() bool {
	return s.ExpiresAt != nil && time.Now().After(*s.ExpiresAt)
}

// IsViewLimitReached checks if max views has been reached
func (s *ShareLink) IsViewLimitReached() bool {
	return s.MaxViews != nil && s.ViewCount >= *s.MaxViews
}

// IsIPLimitReached checks if max unique IPs has been reached
func (s *ShareLink) IsIPLimitReached(currentUniqueIPs int) bool {
	return s.MaxIPs != nil && currentUniqueIPs >= *s.MaxIPs
}

// ShareLinkAccess records individual access to a share link
type ShareLinkAccess struct {
	ID          int64
	ShareLinkID int64
	IPAddress   string
	UserAgent   string
	AccessedAt  time.Time
}

// ShareLinkStats provides aggregated statistics for a share link
type ShareLinkStats struct {
	TotalViews int
	UniqueIPs  int
	LastAccess *time.Time
}
