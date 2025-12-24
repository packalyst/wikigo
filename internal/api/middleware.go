package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"gowiki/internal/config"
	"gowiki/internal/database"
	"gowiki/internal/models"
)

// authRateLimiter tracks failed authentication attempts per IP.
type authRateLimiter struct {
	attempts map[string]*authAttempt
	mu       sync.RWMutex
	maxFails int
	window   time.Duration
}

type authAttempt struct {
	count     int
	resetAt   time.Time
}

func newAuthRateLimiter(maxFails int, window time.Duration) *authRateLimiter {
	rl := &authRateLimiter{
		attempts: make(map[string]*authAttempt),
		maxFails: maxFails,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *authRateLimiter) check(ip string) bool {
	rl.mu.RLock()
	attempt, exists := rl.attempts[ip]
	rl.mu.RUnlock()

	if !exists {
		return true
	}

	if time.Now().After(attempt.resetAt) {
		return true
	}

	return attempt.count < rl.maxFails
}

func (rl *authRateLimiter) recordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	attempt, exists := rl.attempts[ip]

	if !exists || now.After(attempt.resetAt) {
		rl.attempts[ip] = &authAttempt{
			count:   1,
			resetAt: now.Add(rl.window),
		}
		return
	}

	attempt.count++
}

func (rl *authRateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, attempt := range rl.attempts {
			if now.After(attempt.resetAt) {
				delete(rl.attempts, ip)
			}
		}
		rl.mu.Unlock()
	}
}

var apiAuthLimiter = newAuthRateLimiter(10, 15*time.Minute) // 10 failures per 15 min

type contextKey string

const (
	userContextKey  contextKey = "api_user"
	tokenContextKey contextKey = "api_token"
)

// JWTClaims represents the claims in a JWT token.
type JWTClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// JWTMiddleware handles JWT authentication for the API.
type JWTMiddleware struct {
	db     *database.DB
	config *config.Config
}

// NewJWTMiddleware creates a new JWT middleware.
func NewJWTMiddleware(db *database.DB, cfg *config.Config) *JWTMiddleware {
	return &JWTMiddleware{
		db:     db,
		config: cfg,
	}
}

// Middleware returns the Echo middleware function.
func (m *JWTMiddleware) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			clientIP := c.RealIP()

			// Check rate limit before processing
			if !apiAuthLimiter.check(clientIP) {
				return echo.NewHTTPError(http.StatusTooManyRequests, "too many failed authentication attempts")
			}

			// Get Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				apiAuthLimiter.recordFailure(clientIP)
				return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
			}

			// Check for Bearer token
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				apiAuthLimiter.recordFailure(clientIP)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid authorization format")
			}

			tokenString := parts[1]

			// Try JWT first
			claims := &JWTClaims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				return []byte(m.config.Security.SecretKey), nil
			})

			if err == nil && token.Valid {
				// JWT is valid, get user
				user, err := m.db.GetUserByID(c.Request().Context(), claims.UserID)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user")
				}
				if user == nil || !user.IsActive {
					apiAuthLimiter.recordFailure(clientIP)
					return echo.NewHTTPError(http.StatusUnauthorized, "user not found or inactive")
				}

				// Set user in context
				ctx := context.WithValue(c.Request().Context(), userContextKey, user)
				c.SetRequest(c.Request().WithContext(ctx))
				return next(c)
			}

			// Try API token (hash the token and look it up)
			tokenHash := hashToken(tokenString)
			apiToken, err := m.db.GetAPITokenByHash(c.Request().Context(), tokenHash)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to validate token")
			}
			if apiToken == nil {
				apiAuthLimiter.recordFailure(clientIP)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}

			// Check expiration
			if time.Now().After(apiToken.ExpiresAt) {
				return echo.NewHTTPError(http.StatusUnauthorized, "token expired")
			}

			// Get user
			user, err := m.db.GetUserByID(c.Request().Context(), apiToken.UserID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user")
			}
			if user == nil || !user.IsActive {
				return echo.NewHTTPError(http.StatusUnauthorized, "user not found or inactive")
			}

			// Update last used
			go m.db.UpdateAPITokenLastUsed(context.Background(), apiToken.ID)

			// Set user and token in context
			ctx := context.WithValue(c.Request().Context(), userContextKey, user)
			ctx = context.WithValue(ctx, tokenContextKey, apiToken)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// OptionalMiddleware allows requests without auth but sets user if present.
func (m *JWTMiddleware) OptionalMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return next(c)
			}

			// Try to authenticate, but don't fail if it doesn't work
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				return next(c)
			}

			tokenString := parts[1]

			// Try JWT
			claims := &JWTClaims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				return []byte(m.config.Security.SecretKey), nil
			})

			if err == nil && token.Valid {
				user, err := m.db.GetUserByID(c.Request().Context(), claims.UserID)
				if err == nil && user != nil && user.IsActive {
					ctx := context.WithValue(c.Request().Context(), userContextKey, user)
					c.SetRequest(c.Request().WithContext(ctx))
				}
			}

			return next(c)
		}
	}
}

// RequireScope middleware checks that the API token has the required scope.
func RequireScope(scope string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := GetAPIToken(c)
			if token == nil {
				// Using JWT, allow based on user role
				user := GetAPIUser(c)
				if user == nil {
					return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
				}
				// JWT users have full access based on role
				return next(c)
			}

			if !token.HasScope(scope) {
				return echo.NewHTTPError(http.StatusForbidden, "insufficient scope")
			}

			return next(c)
		}
	}
}

// RequireRole middleware checks that the user has the required role.
func RequireRole(role models.Role) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetAPIUser(c)
			if user == nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}

			hasRole := false
			switch role {
			case models.RoleViewer:
				hasRole = true
			case models.RoleEditor:
				hasRole = user.Role.CanEdit()
			case models.RoleAdmin:
				hasRole = user.Role.CanAdmin()
			}

			if !hasRole {
				return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions")
			}

			return next(c)
		}
	}
}

// GetAPIUser returns the authenticated user from context.
func GetAPIUser(c echo.Context) *models.User {
	user, _ := c.Request().Context().Value(userContextKey).(*models.User)
	return user
}

// GetAPIToken returns the API token from context (nil if using JWT).
func GetAPIToken(c echo.Context) *models.APIToken {
	token, _ := c.Request().Context().Value(tokenContextKey).(*models.APIToken)
	return token
}

// GenerateJWT creates a new JWT token for a user.
func GenerateJWT(user *models.User, secretKey string, expiry time.Duration) (string, error) {
	claims := &JWTClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     string(user.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "gowiki",
			Subject:   user.Username,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secretKey))
}

// hashToken creates a SHA256 hash of the token for storage.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// HashToken is exported for use in handlers.
func HashToken(token string) string {
	return hashToken(token)
}
