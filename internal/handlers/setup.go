package handlers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/views/setup"
)

// SetupPage renders the initial setup page.
func (h *Handlers) SetupPage(c echo.Context) error {
	// Redirect away if setup is already complete
	ctx := c.Request().Context()
	db := h.wikiService.GetDB()
	if complete, _ := db.GetSetting(ctx, "setup_complete"); complete == "true" {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	data := setup.SetupData{
		SiteName:  h.config.Site.Name,
		CSRFToken: middleware.GetCSRFToken(c),
	}
	return render(c, http.StatusOK, setup.SetupPage(data))
}

// SetupSubmit handles the setup form submission.
func (h *Handlers) SetupSubmit(c echo.Context) error {
	// Redirect away if setup is already complete
	ctx := c.Request().Context()
	db := h.wikiService.GetDB()
	if complete, _ := db.GetSetting(ctx, "setup_complete"); complete == "true" {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	username := strings.TrimSpace(c.FormValue("username"))
	email := strings.TrimSpace(c.FormValue("email"))
	password := c.FormValue("password")
	passwordConfirm := c.FormValue("password_confirm")

	// Validate passwords match
	if password != passwordConfirm {
		data := setup.SetupData{
			SiteName:  h.config.Site.Name,
			Error:     "Passwords do not match",
			Username:  username,
			Email:     email,
			CSRFToken: middleware.GetCSRFToken(c),
		}
		return render(c, http.StatusOK, setup.SetupPage(data))
	}

	// Create initial admin user (bypasses reserved username check)
	_, err := h.authService.CreateInitialAdmin(ctx, models.UserCreate{
		Username: username,
		Email:    email,
		Password: password,
	})

	if err != nil {
		data := setup.SetupData{
			SiteName:  h.config.Site.Name,
			Error:     err.Error(),
			Username:  username,
			Email:     email,
			CSRFToken: middleware.GetCSRFToken(c),
		}
		return render(c, http.StatusOK, setup.SetupPage(data))
	}

	// Mark setup as complete
	if err := db.SetSetting(ctx, "setup_complete", "true"); err != nil {
		data := setup.SetupData{
			SiteName:  h.config.Site.Name,
			Error:     "Failed to complete setup: " + err.Error(),
			Username:  username,
			Email:     email,
			CSRFToken: middleware.GetCSRFToken(c),
		}
		return render(c, http.StatusOK, setup.SetupPage(data))
	}

	// Import documentation
	user, _ := db.GetUserByUsername(ctx, username)
	if user != nil {
		if err := h.wikiService.ImportDocumentation(ctx, user.ID); err != nil {
			// Log but don't fail - admin can import later
			c.Logger().Warnf("Failed to import documentation: %v", err)
		}
	}

	// Redirect to login
	return c.Redirect(http.StatusSeeOther, "/login?setup=complete")
}
