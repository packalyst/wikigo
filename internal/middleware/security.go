package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// SecurityHeaders middleware adds security-related HTTP headers.
func SecurityHeaders() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()

			// Prevent clickjacking
			h.Set("X-Frame-Options", "SAMEORIGIN")

			// Prevent MIME-type sniffing
			h.Set("X-Content-Type-Options", "nosniff")

			// Enable XSS filter in browsers
			h.Set("X-XSS-Protection", "1; mode=block")

			// Control referrer information
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Permissions policy (formerly Feature-Policy)
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			// Content Security Policy - restrictive but allows necessary functionality
			csp := strings.Join([]string{
				"default-src 'self'",
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'",                // Allow inline scripts for HTMX, eval for Alpine.js
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com", // Allow inline styles for Tailwind + Google Fonts
				"img-src 'self' data: https:",                                   // Allow images from self, data URIs, and HTTPS
				"font-src 'self' https://fonts.gstatic.com",                     // Allow Google Fonts
				"connect-src 'self'",                                            // Allow AJAX/fetch to self
				"frame-ancestors 'self'",
				"base-uri 'self'",
				"form-action 'self'",
			}, "; ")
			h.Set("Content-Security-Policy", csp)

			return next(c)
		}
	}
}

// CSRF provides Cross-Site Request Forgery protection.
type CSRF struct {
	sessionManager *SessionManager
	tokenLength    int
}

// NewCSRF creates a new CSRF protection middleware.
func NewCSRF(sm *SessionManager) *CSRF {
	return &CSRF{
		sessionManager: sm,
		tokenLength:    32,
	}
}

// Middleware returns the CSRF middleware function.
func (csrf *CSRF) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip CSRF for safe methods
			if isSafeMethod(c.Request().Method) {
				// Generate token for forms
				token, err := csrf.getOrCreateToken(c)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate CSRF token")
				}
				c.Set("csrf_token", token)
				return next(c)
			}

			// Validate CSRF token for unsafe methods
			session, err := csrf.sessionManager.GetSession(c)
			if err != nil {
				return echo.NewHTTPError(http.StatusForbidden, "Invalid session")
			}

			expectedToken, ok := session.Values["csrf_token"].(string)
			if !ok || expectedToken == "" {
				return echo.NewHTTPError(http.StatusForbidden, "CSRF token missing from session")
			}

			// Check token from header first, then form
			actualToken := c.Request().Header.Get("X-CSRF-Token")
			if actualToken == "" {
				actualToken = c.FormValue("csrf_token")
			}

			if actualToken == "" {
				return echo.NewHTTPError(http.StatusForbidden, "CSRF token missing from request")
			}

			// Constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(expectedToken), []byte(actualToken)) != 1 {
				return echo.NewHTTPError(http.StatusForbidden, "Invalid CSRF token")
			}

			// Keep the existing token in context for any forms rendered after POST
			// Note: Token rotation removed to prevent session corruption from multiple Save() calls
			c.Set("csrf_token", expectedToken)

			return next(c)
		}
	}
}

// getOrCreateToken retrieves or creates a CSRF token.
func (csrf *CSRF) getOrCreateToken(c echo.Context) (string, error) {
	session, err := csrf.sessionManager.GetSession(c)
	if err != nil {
		// Log the error but try to continue with a new session
		c.Logger().Warnf("Failed to get session for CSRF: %v", err)
		// Generate a token anyway for this request
		return csrf.generateToken()
	}

	token, ok := session.Values["csrf_token"].(string)
	if !ok || token == "" {
		token, err = csrf.generateToken()
		if err != nil {
			return "", err
		}
		session.Values["csrf_token"] = token
		if err := session.Save(c.Request(), c.Response()); err != nil {
			// Log but don't fail - the token can still be used for this request
			c.Logger().Warnf("Failed to save session with CSRF token: %v", err)
		}
	}

	return token, nil
}

// generateToken creates a cryptographically secure random token.
func (csrf *CSRF) generateToken() (string, error) {
	bytes := make([]byte, csrf.tokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// isSafeMethod returns true for HTTP methods that don't modify state.
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// GetCSRFToken retrieves the CSRF token from context.
func GetCSRFToken(c echo.Context) string {
	token, ok := c.Get("csrf_token").(string)
	if !ok {
		return ""
	}
	return token
}

// RateLimiter provides request rate limiting.
type RateLimiter struct {
	requests    map[string]*rateLimitEntry
	mu          sync.RWMutex
	maxRequests int
	window      time.Duration
}

type rateLimitEntry struct {
	count     int
	expiresAt time.Time
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests:    make(map[string]*rateLimitEntry),
		maxRequests: maxRequests,
		window:      window,
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// Middleware returns the rate limiting middleware.
func (rl *RateLimiter) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get client identifier (IP address)
			clientIP := c.RealIP()

			// Check rate limit
			if !rl.allow(clientIP) {
				c.Response().Header().Set("Retry-After", fmt.Sprintf("%d", int(rl.window.Seconds())))
				return echo.NewHTTPError(http.StatusTooManyRequests, "Rate limit exceeded")
			}

			return next(c)
		}
	}
}

// allow checks if a request should be allowed.
func (rl *RateLimiter) allow(clientID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	entry, exists := rl.requests[clientID]
	if !exists || now.After(entry.expiresAt) {
		// Create new entry
		rl.requests[clientID] = &rateLimitEntry{
			count:     1,
			expiresAt: now.Add(rl.window),
		}
		return true
	}

	// Check if limit exceeded
	if entry.count >= rl.maxRequests {
		return false
	}

	entry.count++
	return true
}

// cleanup removes expired entries periodically.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for key, entry := range rl.requests {
			if now.After(entry.expiresAt) {
				delete(rl.requests, key)
			}
		}
		rl.mu.Unlock()
	}
}

// LoginRateLimiter provides stricter rate limiting for login attempts.
type LoginRateLimiter struct {
	attempts    map[string]*loginAttempt
	mu          sync.RWMutex
	maxAttempts int
	lockoutTime time.Duration
}

type loginAttempt struct {
	count     int
	lockedAt  time.Time
	lastTry   time.Time
}

// NewLoginRateLimiter creates a rate limiter specifically for login attempts.
func NewLoginRateLimiter(maxAttempts int, lockoutTime time.Duration) *LoginRateLimiter {
	lrl := &LoginRateLimiter{
		attempts:    make(map[string]*loginAttempt),
		maxAttempts: maxAttempts,
		lockoutTime: lockoutTime,
	}

	go lrl.cleanup()

	return lrl
}

// Check returns true if login attempt should be allowed.
func (lrl *LoginRateLimiter) Check(identifier string) (bool, time.Duration) {
	lrl.mu.Lock()
	defer lrl.mu.Unlock()

	now := time.Now()

	attempt, exists := lrl.attempts[identifier]
	if !exists {
		lrl.attempts[identifier] = &loginAttempt{
			count:   0,
			lastTry: now,
		}
		return true, 0
	}

	// Check if in lockout period
	if !attempt.lockedAt.IsZero() {
		remaining := attempt.lockedAt.Add(lrl.lockoutTime).Sub(now)
		if remaining > 0 {
			return false, remaining
		}
		// Lockout expired, reset
		attempt.count = 0
		attempt.lockedAt = time.Time{}
	}

	return true, 0
}

// RecordFailure records a failed login attempt.
func (lrl *LoginRateLimiter) RecordFailure(identifier string) {
	lrl.mu.Lock()
	defer lrl.mu.Unlock()

	now := time.Now()

	attempt, exists := lrl.attempts[identifier]
	if !exists {
		lrl.attempts[identifier] = &loginAttempt{
			count:   1,
			lastTry: now,
		}
		return
	}

	attempt.count++
	attempt.lastTry = now

	if attempt.count >= lrl.maxAttempts {
		attempt.lockedAt = now
	}
}

// RecordSuccess clears failed attempts after successful login.
func (lrl *LoginRateLimiter) RecordSuccess(identifier string) {
	lrl.mu.Lock()
	defer lrl.mu.Unlock()

	delete(lrl.attempts, identifier)
}

// cleanup removes old entries.
func (lrl *LoginRateLimiter) cleanup() {
	ticker := time.NewTicker(lrl.lockoutTime)
	defer ticker.Stop()

	for range ticker.C {
		lrl.mu.Lock()
		now := time.Now()
		for key, attempt := range lrl.attempts {
			// Remove entries that haven't been used in a while
			if now.Sub(attempt.lastTry) > lrl.lockoutTime*2 {
				delete(lrl.attempts, key)
			}
		}
		lrl.mu.Unlock()
	}
}
