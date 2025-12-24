package middleware

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
)

// RequestLogger logs HTTP requests with timing and user info.
func RequestLogger() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

			// Calculate request duration
			duration := time.Since(start)

			// Get request info
			req := c.Request()
			res := c.Response()

			// Get user info if available
			userInfo := "anonymous"
			if user := GetUser(c); user != nil {
				userInfo = user.Username
			}

			// Determine status code
			status := res.Status
			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				} else {
					status = 500
				}
			}

			// Get request ID if available
			reqID := GetRequestID(c)

			// Format log entry
			logEntry := fmt.Sprintf("%s | %s | %3d | %12s | %15s | %-7s | %s",
				time.Now().Format("2006-01-02 15:04:05"),
				reqID,
				status,
				duration.Round(time.Microsecond),
				c.RealIP(),
				req.Method,
				req.URL.Path,
			)

			// Add user info for authenticated requests
			if userInfo != "anonymous" {
				logEntry += fmt.Sprintf(" | user=%s", userInfo)
			}

			// Add query string if present
			if req.URL.RawQuery != "" {
				logEntry += fmt.Sprintf(" | query=%s", req.URL.RawQuery)
			}

			// Log with color coding based on status
			switch {
			case status >= 500:
				fmt.Printf("\033[31m%s\033[0m\n", logEntry) // Red for 5xx
			case status >= 400:
				fmt.Printf("\033[33m%s\033[0m\n", logEntry) // Yellow for 4xx
			case status >= 300:
				fmt.Printf("\033[36m%s\033[0m\n", logEntry) // Cyan for 3xx
			default:
				fmt.Printf("\033[32m%s\033[0m\n", logEntry) // Green for 2xx
			}

			return err
		}
	}
}

// RecoveryMiddleware recovers from panics and logs them.
func RecoveryMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					// Get request ID for tracing
					reqID := GetRequestID(c)

					// Log the panic with request ID
					fmt.Printf("\033[31m[PANIC] [%s] %v\033[0m\n", reqID, r)

					// Get request info for context
					req := c.Request()
					fmt.Printf("\033[31m[PANIC] [%s] Request: %s %s\033[0m\n", reqID, req.Method, req.URL.Path)

					// Return 500 error
					c.Error(echo.NewHTTPError(500, "Internal server error"))
				}
			}()

			return next(c)
		}
	}
}
