package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"gowiki/internal/api"
	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/views/pages"
)

// TokensPage renders the API tokens management page.
func (h *Handlers) TokensPage(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil {
		return c.Redirect(http.StatusSeeOther, "/login")
	}

	tokens, err := h.wikiService.GetDB().ListAPITokensByUser(c.Request().Context(), user.ID)
	if err != nil {
		tokens = []models.APIToken{}
	}

	// Check for newly created token in flash
	newTokenFlash := h.sessionManager.GetFlash(c, "new_token")
	newToken := ""
	if len(newTokenFlash) > 0 {
		newToken = newTokenFlash[0]
	}

	data := pages.TokensData{
		PageData: h.basePageData(c, "API Tokens"),
		Tokens:   tokens,
		NewToken: newToken,
	}

	return render(c, http.StatusOK, pages.Tokens(data))
}

// CreateToken creates a new API token.
func (h *Handlers) CreateToken(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil {
		return c.Redirect(http.StatusSeeOther, "/login")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = "Unnamed Token"
	}

	// Get scopes from checkboxes
	scopeValues := c.Request().Form["scopes"]
	scopes := strings.Join(scopeValues, ",")
	if scopes == "" {
		scopes = "read"
	}

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		h.setFlash(c, "error", "Failed to generate token")
		return c.Redirect(http.StatusSeeOther, "/tokens")
	}
	rawToken := hex.EncodeToString(tokenBytes)
	tokenHash := api.HashToken(rawToken)

	token := &models.APIToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		Name:      name,
		Scopes:    scopes,
		ExpiresAt: time.Now().Add(h.config.Security.APITokenExpiry),
	}

	if err := h.wikiService.GetDB().CreateAPIToken(c.Request().Context(), token); err != nil {
		h.setFlash(c, "error", "Failed to create token")
		return c.Redirect(http.StatusSeeOther, "/tokens")
	}

	// Store token in flash and redirect (PRG pattern)
	h.sessionManager.SetFlash(c, "new_token", rawToken)
	return c.Redirect(http.StatusSeeOther, "/tokens")
}

// DeleteToken revokes an API token.
func (h *Handlers) DeleteToken(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid token ID")
	}

	// Verify token belongs to user
	tokens, _ := h.wikiService.GetDB().ListAPITokensByUser(c.Request().Context(), user.ID)
	found := false
	for _, t := range tokens {
		if t.ID == tokenID {
			found = true
			break
		}
	}

	if !found {
		return echo.NewHTTPError(http.StatusForbidden, "token not found")
	}

	if err := h.wikiService.GetDB().DeleteAPIToken(c.Request().Context(), tokenID); err != nil {
		c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"Failed to revoke token","type":"error"}}`)
		return c.NoContent(http.StatusInternalServerError)
	}

	c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"Token revoked successfully","type":"success"}}`)
	return c.NoContent(http.StatusOK)
}
