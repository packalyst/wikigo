package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/labstack/echo/v4"
)

// RequestIDKey is the context key for request ID.
const RequestIDKey = "request_id"

// RequestID generates a unique request ID for each request.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Check if X-Request-ID header is already set
			reqID := c.Request().Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = generateRequestID()
			}

			// Set request ID in context and response header
			c.Set(RequestIDKey, reqID)
			c.Response().Header().Set("X-Request-ID", reqID)

			return next(c)
		}
	}
}

// GetRequestID returns the request ID from context.
func GetRequestID(c echo.Context) string {
	if id, ok := c.Get(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// generateRequestID creates a unique request ID.
func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}
