package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/models"
	"gowiki/internal/services"
	"gowiki/internal/views/auth"
)

// Input length limits for auth
const (
	maxUsernameLength = 50
	maxEmailLength    = 255
	minPasswordLength = 8
	maxPasswordLength = 128
)

// LoginForm renders the login page.
func (h *Handlers) LoginForm(c echo.Context) error {
	next := c.QueryParam("next")

	data := auth.LoginData{
		PageData:          h.basePageData(c, "Login"),
		Next:              next,
		AllowRegistration: h.config.Site.AllowRegistration,
	}

	return render(c, http.StatusOK, auth.Login(data))
}

// Login handles the login form submission.
func (h *Handlers) Login(c echo.Context) error {
	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	next := c.FormValue("next")

	// Rate limiting check
	clientIP := c.RealIP()
	allowed, remaining := h.loginLimiter.Check(clientIP)
	if !allowed {
		data := auth.LoginData{
			PageData: h.basePageData(c, "Login"),
			Error:    "Too many login attempts. Please try again in " + formatDuration(remaining) + ".",
			Next:     next,
			Username: username,
		}
		return render(c, http.StatusTooManyRequests, auth.Login(data))
	}

	// Validate input
	if username == "" || password == "" {
		data := auth.LoginData{
			PageData: h.basePageData(c, "Login"),
			Error:    "Username and password are required.",
			Next:     next,
			Username: username,
		}
		return render(c, http.StatusBadRequest, auth.Login(data))
	}

	// Authenticate
	user, err := h.authService.Authenticate(c.Request().Context(), username, password)
	if err != nil {
		// Record failed attempt
		h.loginLimiter.RecordFailure(clientIP)

		errorMsg := "Invalid username or password."
		if errors.Is(err, services.ErrUserInactive) {
			errorMsg = "Your account has been deactivated."
		}

		data := auth.LoginData{
			PageData: h.basePageData(c, "Login"),
			Error:    errorMsg,
			Next:     next,
			Username: username,
		}
		return render(c, http.StatusUnauthorized, auth.Login(data))
	}

	// Clear rate limit on success
	h.loginLimiter.RecordSuccess(clientIP)

	// Set session
	if err := h.sessionManager.SetUserID(c, user.ID); err != nil {
		data := auth.LoginData{
			PageData: h.basePageData(c, "Login"),
			Error:    "Failed to create session. Please try again.",
			Next:     next,
			Username: username,
		}
		return render(c, http.StatusInternalServerError, auth.Login(data))
	}

	// Redirect to next page or home
	redirectURL := "/"
	if next != "" && isValidRedirect(next) {
		redirectURL = next
	}

	return c.Redirect(http.StatusSeeOther, redirectURL)
}

// Logout handles user logout.
func (h *Handlers) Logout(c echo.Context) error {
	if err := h.sessionManager.ClearSession(c); err != nil {
		// Log error but continue
	}

	return c.Redirect(http.StatusSeeOther, "/")
}

// RegisterForm renders the registration page.
func (h *Handlers) RegisterForm(c echo.Context) error {
	if !h.config.Site.AllowRegistration {
		return c.Redirect(http.StatusSeeOther, "/login")
	}

	data := auth.RegisterData{
		PageData: h.basePageData(c, "Register"),
		Errors:   make(map[string]string),
	}

	return render(c, http.StatusOK, auth.Register(data))
}

// Register handles the registration form submission.
func (h *Handlers) Register(c echo.Context) error {
	if !h.config.Site.AllowRegistration {
		return c.Redirect(http.StatusSeeOther, "/login")
	}

	username := strings.TrimSpace(c.FormValue("username"))
	email := strings.TrimSpace(c.FormValue("email"))
	password := c.FormValue("password")
	passwordConfirm := c.FormValue("password_confirm")

	errs := make(map[string]string)

	// Validate length limits
	if len(username) == 0 {
		errs["username"] = "Username is required."
	} else if len(username) > maxUsernameLength {
		errs["username"] = "Username must be less than 50 characters."
	}
	if len(email) == 0 {
		errs["email"] = "Email is required."
	} else if len(email) > maxEmailLength {
		errs["email"] = "Email must be less than 255 characters."
	}
	if len(password) < minPasswordLength {
		errs["password"] = "Password must be at least 8 characters."
	} else if len(password) > maxPasswordLength {
		errs["password"] = "Password must be less than 128 characters."
	}

	// Validate passwords match
	if password != passwordConfirm {
		errs["password_confirm"] = "Passwords do not match."
	}

	if len(errs) > 0 {
		data := auth.RegisterData{
			PageData:   h.basePageData(c, "Register"),
			Errors:     errs,
			FormValues: auth.RegisterFormValues{Username: username, Email: email},
		}
		return render(c, http.StatusBadRequest, auth.Register(data))
	}

	// Create user
	user, err := h.authService.CreateUser(c.Request().Context(), models.UserCreate{
		Username: username,
		Email:    email,
		Password: password,
		Role:     models.Role(h.config.Site.DefaultRole),
	})

	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidUsername):
			errs["username"] = err.Error()
		case errors.Is(err, services.ErrInvalidEmail):
			errs["email"] = err.Error()
		case errors.Is(err, services.ErrInvalidPassword):
			errs["password"] = err.Error()
		case errors.Is(err, services.ErrUserExists):
			errs["username"] = "Username or email already exists."
		default:
			errs["username"] = "Registration failed. Please try again."
		}

		data := auth.RegisterData{
			PageData:   h.basePageData(c, "Register"),
			Errors:     errs,
			FormValues: auth.RegisterFormValues{Username: username, Email: email},
		}
		return render(c, http.StatusBadRequest, auth.Register(data))
	}

	// Auto-login after registration
	if err := h.sessionManager.SetUserID(c, user.ID); err != nil {
		h.setFlash(c, "success", "Account created! Please log in.")
		return c.Redirect(http.StatusSeeOther, "/login")
	}

	h.setFlash(c, "success", "Welcome to "+h.config.Site.Name+"!")
	return c.Redirect(http.StatusSeeOther, "/")
}

// isValidRedirect checks if a redirect URL is safe.
func isValidRedirect(rawURL string) bool {
	// Only allow relative URLs starting with /
	if !strings.HasPrefix(rawURL, "/") {
		return false
	}

	// Prevent open redirect via protocol-relative URLs
	if strings.HasPrefix(rawURL, "//") {
		return false
	}

	// Parse URL to ensure it's truly relative (no host)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Ensure no scheme or host (prevents tricks like /\example.com)
	if parsed.Scheme != "" || parsed.Host != "" {
		return false
	}

	// Ensure path doesn't contain backslashes (Windows path tricks)
	if strings.Contains(rawURL, "\\") {
		return false
	}

	// Prevent redirect to login/logout to avoid loops
	if parsed.Path == "/login" || parsed.Path == "/logout" || parsed.Path == "/register" {
		return false
	}

	return true
}

// formatDuration formats a duration for display.
func formatDuration(d interface{}) string {
	// Simple duration formatting
	return "15 minutes"
}
