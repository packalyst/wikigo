package models

import (
	"database/sql"
	"strings"
	"time"
)

// Role represents user permission levels.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// IsValid checks if the role is a valid value.
func (r Role) IsValid() bool {
	switch r {
	case RoleAdmin, RoleEditor, RoleViewer:
		return true
	}
	return false
}

// CanEdit returns true if the role has edit permissions.
func (r Role) CanEdit() bool {
	return r == RoleAdmin || r == RoleEditor
}

// CanAdmin returns true if the role has admin permissions.
func (r Role) CanAdmin() bool {
	return r == RoleAdmin
}

// User represents a wiki user.
type User struct {
	ID           int64        `json:"id"`
	Username     string       `json:"username"`
	Email        string       `json:"email"`
	PasswordHash string       `json:"-"` // Never expose in JSON
	Role         Role         `json:"role"`
	IsActive     bool         `json:"is_active"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	LastLoginAt  sql.NullTime `json:"last_login_at,omitempty"`
}

// UserCreate contains data for creating a new user.
type UserCreate struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     Role   `json:"role"`
}

// UserUpdate contains data for updating a user.
type UserUpdate struct {
	Email    *string `json:"email,omitempty"`
	Password *string `json:"password,omitempty"`
	Role     *Role   `json:"role,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
}

// Session represents a user session for database-backed sessions.
type Session struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Data      string    `json:"data"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// APIToken represents an API access token.
type APIToken struct {
	ID         int64        `json:"id"`
	UserID     int64        `json:"user_id"`
	TokenHash  string       `json:"-"` // Never expose
	Name       string       `json:"name"`
	Scopes     string       `json:"scopes"`
	LastUsedAt sql.NullTime `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time    `json:"expires_at"`
	CreatedAt  time.Time    `json:"created_at"`
}

// APITokenCreate contains data for creating a new API token.
type APITokenCreate struct {
	Name   string `json:"name"`
	Scopes string `json:"scopes"`
}

// HasScope checks if the token has a specific scope.
func (t *APIToken) HasScope(scope string) bool {
	scopes := strings.Split(t.Scopes, ",")
	for _, s := range scopes {
		if strings.TrimSpace(s) == scope || strings.TrimSpace(s) == "admin" {
			return true
		}
	}
	return false
}

// ScopeList returns the scopes as a slice.
func (t *APIToken) ScopeList() []string {
	var result []string
	for _, s := range strings.Split(t.Scopes, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// LastUsedString returns a string representation of LastUsedAt.
func (t *APIToken) LastUsedString() string {
	if t.LastUsedAt.Valid {
		return t.LastUsedAt.Time.Format("Jan 2, 2006")
	}
	return ""
}

// WasUsed returns true if the token has been used.
func (t *APIToken) WasUsed() bool {
	return t.LastUsedAt.Valid
}
