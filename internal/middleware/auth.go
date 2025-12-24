package middleware

import (
	"context"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"

	"gowiki/internal/config"
	"gowiki/internal/models"
	"gowiki/internal/services"
)

// Context keys for user data.
type contextKey string

const (
	userContextKey    contextKey = "user"
	sessionContextKey contextKey = "session"
)

// SessionManager handles secure session management.
type SessionManager struct {
	store       *sessions.CookieStore
	sessionName string
	authService *services.AuthService
}

// NewSessionManager creates a new session manager.
func NewSessionManager(cfg *config.Config, authService *services.AuthService) *SessionManager {
	// Create cookie store with secure key
	store := sessions.NewCookieStore([]byte(cfg.Security.SecretKey))

	// Configure secure session options
	isHTTPS := len(cfg.Site.URL) >= 5 && cfg.Site.URL[:5] == "https"
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   cfg.Security.SessionMaxAge,
		HttpOnly: true,                 // Prevent JavaScript access
		Secure:   isHTTPS,              // Secure only for HTTPS
		SameSite: http.SameSiteLaxMode, // CSRF protection
	}

	return &SessionManager{
		store:       store,
		sessionName: cfg.Security.SessionName,
		authService: authService,
	}
}

// GetSession retrieves the current session.
func (sm *SessionManager) GetSession(c echo.Context) (*sessions.Session, error) {
	return sm.store.Get(c.Request(), sm.sessionName)
}

// SetUserID stores the user ID in the session.
func (sm *SessionManager) SetUserID(c echo.Context, userID int64) error {
	session, err := sm.GetSession(c)
	if err != nil {
		return err
	}

	session.Values["user_id"] = userID
	return session.Save(c.Request(), c.Response())
}

// GetUserID retrieves the user ID from the session.
func (sm *SessionManager) GetUserID(c echo.Context) (int64, bool) {
	session, err := sm.GetSession(c)
	if err != nil {
		return 0, false
	}

	userID, ok := session.Values["user_id"].(int64)
	return userID, ok
}

// ClearSession removes all session data.
func (sm *SessionManager) ClearSession(c echo.Context) error {
	session, err := sm.GetSession(c)
	if err != nil {
		return err
	}

	session.Values = make(map[interface{}]interface{})
	session.Options.MaxAge = -1 // Delete cookie

	return session.Save(c.Request(), c.Response())
}

// SetFlash sets a flash message in the session.
func (sm *SessionManager) SetFlash(c echo.Context, key, message string) error {
	session, err := sm.GetSession(c)
	if err != nil {
		return err
	}

	session.AddFlash(message, key)
	return session.Save(c.Request(), c.Response())
}

// GetFlash retrieves and clears flash messages.
func (sm *SessionManager) GetFlash(c echo.Context, key string) []string {
	session, err := sm.GetSession(c)
	if err != nil {
		return nil
	}

	flashes := session.Flashes(key)
	session.Save(c.Request(), c.Response())

	messages := make([]string, 0, len(flashes))
	for _, f := range flashes {
		if msg, ok := f.(string); ok {
			messages = append(messages, msg)
		}
	}

	return messages
}

// AuthMiddleware loads the current user from session.
func (sm *SessionManager) AuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, ok := sm.GetUserID(c)
			if !ok {
				return next(c)
			}

			user, err := sm.authService.GetUserByID(c.Request().Context(), userID)
			if err != nil || user == nil || !user.IsActive {
				// Invalid session, clear it
				sm.ClearSession(c)
				return next(c)
			}

			// Store user in context
			ctx := context.WithValue(c.Request().Context(), userContextKey, user)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// RequireAuth middleware ensures user is authenticated.
func RequireAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if GetUser(c) == nil {
				return redirectToLogin(c)
			}
			return next(c)
		}
	}
}

// RequireRole middleware ensures user has at least the specified role.
func RequireRole(minRole models.Role) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return redirectToLogin(c)
			}

			hasPermission := false
			switch minRole {
			case models.RoleViewer:
				hasPermission = true
			case models.RoleEditor:
				hasPermission = user.Role.CanEdit()
			case models.RoleAdmin:
				hasPermission = user.Role.CanAdmin()
			}

			if !hasPermission {
				return echo.NewHTTPError(http.StatusForbidden, "Insufficient permissions")
			}

			return next(c)
		}
	}
}

// RequireNoAuth middleware ensures user is NOT authenticated (for login page).
func RequireNoAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user != nil {
				return c.Redirect(http.StatusSeeOther, "/")
			}
			return next(c)
		}
	}
}

// GetUser retrieves the current user from context.
func GetUser(c echo.Context) *models.User {
	user, ok := c.Request().Context().Value(userContextKey).(*models.User)
	if !ok {
		return nil
	}
	return user
}

// redirectToLogin redirects to the login page with a next parameter.
func redirectToLogin(c echo.Context) error {
	return c.Redirect(http.StatusSeeOther, "/login?next="+c.Request().URL.Path)
}

// IsAuthenticated returns true if a user is logged in.
func IsAuthenticated(c echo.Context) bool {
	return GetUser(c) != nil
}

// CanEdit returns true if the current user can edit content.
func CanEdit(c echo.Context) bool {
	user := GetUser(c)
	return user != nil && user.Role.CanEdit()
}

// CanAdmin returns true if the current user has admin privileges.
func CanAdmin(c echo.Context) bool {
	user := GetUser(c)
	return user != nil && user.Role.CanAdmin()
}

// RequireAuthIfPrivate middleware requires authentication if the wiki is set to private mode.
func RequireAuthIfPrivate(cfg *config.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if cfg.Site.RequireAuth && GetUser(c) == nil {
				return redirectToLogin(c)
			}
			return next(c)
		}
	}
}
