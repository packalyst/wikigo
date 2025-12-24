package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/database"
)

// SetupRequired creates middleware that redirects to setup if not complete.
func SetupRequired(db *database.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path

			// Always allow static files and setup routes
			if strings.HasPrefix(path, "/static/") ||
				strings.HasPrefix(path, "/setup") ||
				path == "/health" {
				return next(c)
			}

			// Check if setup is complete
			ctx := c.Request().Context()
			setupComplete, err := db.GetSetting(ctx, "setup_complete")
			if err != nil {
				// If we can't check, assume setup is needed
				return c.Redirect(http.StatusSeeOther, "/setup")
			}

			if setupComplete != "true" {
				return c.Redirect(http.StatusSeeOther, "/setup")
			}

			return next(c)
		}
	}
}
