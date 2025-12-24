package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/views/pages"
)

// generateSecureToken generates a cryptographically secure random token.
func generateSecureToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	// Use URL-safe base64 encoding without padding
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// ListShares displays all share links for the current user (or all for admins).
func (h *Handlers) ListShares(c echo.Context) error {
	ctx := c.Request().Context()
	user := middleware.GetUser(c)

	var shareLinks []models.ShareLink
	var err error

	// Admins see all shares, editors see only their own
	if user.Role.CanAdmin() {
		shareLinks, err = h.wikiService.GetDB().ListAllShareLinks(ctx, 100, 0)
	} else {
		shareLinks, err = h.wikiService.GetDB().GetShareLinksByUser(ctx, user.ID)
	}

	if err != nil {
		h.setFlash(c, "error", "Failed to load share links")
		return c.Redirect(http.StatusSeeOther, "/")
	}

	// Get all pages for the page selector
	allPages, err := h.wikiService.GetDB().GetAllPageSummaries(ctx)
	if err != nil {
		allPages = []models.PageSummary{}
	}

	data := pages.SharesData{
		PageData:   h.basePageData(c, "Share Links"),
		ShareLinks: shareLinks,
		Pages:      allPages,
		SiteURL:    h.config.Site.URL,
	}

	return pages.Shares(data).Render(ctx, c.Response().Writer)
}

// CreateShareForm displays the form for creating a new share link (HTMX modal).
func (h *Handlers) CreateShareForm(c echo.Context) error {
	ctx := c.Request().Context()
	pageIDStr := c.Param("pageId")

	pageID, err := strconv.ParseInt(pageIDStr, 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid page ID")
	}

	// Get the page
	page, err := h.wikiService.GetDB().GetPageByID(ctx, pageID)
	if err != nil || page == nil {
		return c.String(http.StatusNotFound, "Page not found")
	}

	data := pages.CreateShareFormData{
		PageData: h.basePageData(c, "Share Page"),
		Page:     page,
	}

	return pages.CreateShareForm(data).Render(ctx, c.Response().Writer)
}

// CreateShare creates a new share link.
func (h *Handlers) CreateShare(c echo.Context) error {
	ctx := c.Request().Context()
	user := middleware.GetUser(c)

	// Parse form data
	pageIDStr := c.FormValue("page_id")
	pageID, err := strconv.ParseInt(pageIDStr, 10, 64)
	if err != nil {
		h.setFlash(c, "error", "Invalid page ID")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Verify page exists
	page, err := h.wikiService.GetDB().GetPageByID(ctx, pageID)
	if err != nil || page == nil {
		h.setFlash(c, "error", "Page not found")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Parse options
	includeChildren := c.FormValue("include_children") == "on"

	var maxViews *int
	if mv := c.FormValue("max_views"); mv != "" {
		val, err := strconv.Atoi(mv)
		if err == nil && val > 0 {
			maxViews = &val
		}
	}

	var maxIPs *int
	if mi := c.FormValue("max_ips"); mi != "" {
		val, err := strconv.Atoi(mi)
		if err == nil && val > 0 {
			maxIPs = &val
		}
	}

	var expiresAt *time.Time
	if exp := c.FormValue("expires_in"); exp != "" {
		duration, err := time.ParseDuration(exp)
		if err == nil && duration > 0 {
			t := time.Now().Add(duration)
			expiresAt = &t
		}
	}

	// Generate secure token
	token, err := generateSecureToken()
	if err != nil {
		h.setFlash(c, "error", "Failed to generate share link")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Create share link
	shareLink := &models.ShareLink{
		TokenHash:       middleware.HashToken(token),
		PageID:          pageID,
		CreatedBy:       user.ID,
		IncludeChildren: includeChildren,
		MaxViews:        maxViews,
		MaxIPs:          maxIPs,
		ExpiresAt:       expiresAt,
	}

	if err := h.wikiService.GetDB().CreateShareLink(ctx, shareLink); err != nil {
		h.setFlash(c, "error", "Failed to create share link")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Build the share URL
	shareURL := fmt.Sprintf("%s/s/%s", strings.TrimRight(h.config.Site.URL, "/"), token)

	// For HTMX requests, return the success template
	if c.Request().Header.Get("HX-Request") == "true" {
		data := pages.ShareSuccessData{
			PageTitle:       page.Title,
			IncludeChildren: includeChildren,
			ShareURL:        shareURL,
		}
		return pages.ShareSuccess(data).Render(ctx, c.Response().Writer)
	}

	h.setFlash(c, "success", "Share link created: "+shareURL)
	return c.Redirect(http.StatusSeeOther, "/shares")
}

// RevokeShare revokes a share link.
func (h *Handlers) RevokeShare(c echo.Context) error {
	ctx := c.Request().Context()
	user := middleware.GetUser(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		h.setFlash(c, "error", "Invalid share link ID")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Get the share link to verify ownership
	link, err := h.wikiService.GetDB().GetShareLinkByID(ctx, id)
	if err != nil || link == nil {
		h.setFlash(c, "error", "Share link not found")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Only creator or admin can revoke
	if link.CreatedBy != user.ID && !user.Role.CanAdmin() {
		h.setFlash(c, "error", "You don't have permission to revoke this share link")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	if err := h.wikiService.GetDB().RevokeShareLink(ctx, id); err != nil {
		h.setFlash(c, "error", "Failed to revoke share link")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	h.setFlash(c, "success", "Share link revoked")

	// For HTMX, return updated row or trigger refresh
	if c.Request().Header.Get("HX-Request") == "true" {
		c.Response().Header().Set("HX-Trigger", `{"showToast": {"message": "Share link revoked", "type": "success"}}`)
		return c.HTML(http.StatusOK, `<span class="badge badge-error">Revoked</span>`)
	}

	return c.Redirect(http.StatusSeeOther, "/shares")
}

// DeleteShare permanently deletes a share link.
func (h *Handlers) DeleteShare(c echo.Context) error {
	ctx := c.Request().Context()
	user := middleware.GetUser(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid share link ID")
	}

	// Get the share link to verify ownership
	link, err := h.wikiService.GetDB().GetShareLinkByID(ctx, id)
	if err != nil || link == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Share link not found")
	}

	// Only creator or admin can delete
	if link.CreatedBy != user.ID && !user.Role.CanAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "Permission denied")
	}

	if err := h.wikiService.GetDB().DeleteShareLink(ctx, id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete share link")
	}

	// For HTMX, trigger removal from list
	if c.Request().Header.Get("HX-Request") == "true" {
		c.Response().Header().Set("HX-Trigger", `{"showToast": {"message": "Share link deleted", "type": "success"}}`)
		return c.NoContent(http.StatusOK)
	}

	h.setFlash(c, "success", "Share link deleted")
	return c.Redirect(http.StatusSeeOther, "/shares")
}

// ViewShareStats displays access statistics for a share link.
func (h *Handlers) ViewShareStats(c echo.Context) error {
	ctx := c.Request().Context()
	user := middleware.GetUser(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		h.setFlash(c, "error", "Invalid share link ID")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Get the share link
	link, err := h.wikiService.GetDB().GetShareLinkByID(ctx, id)
	if err != nil || link == nil {
		h.setFlash(c, "error", "Share link not found")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Only creator or admin can view stats
	if link.CreatedBy != user.ID && !user.Role.CanAdmin() {
		h.setFlash(c, "error", "You don't have permission to view this")
		return c.Redirect(http.StatusSeeOther, "/shares")
	}

	// Get access records
	accesses, err := h.wikiService.GetDB().GetShareLinkAccesses(ctx, id, 100, 0)
	if err != nil {
		accesses = []models.ShareLinkAccess{}
	}

	data := pages.ShareStatsData{
		PageData:  h.basePageData(c, "Share Link Stats"),
		ShareLink: link,
		Accesses:  accesses,
		SiteURL:   h.config.Site.URL,
	}

	return pages.ShareStats(data).Render(ctx, c.Response().Writer)
}

// ViewSharedPage renders a page accessed via share link.
func (h *Handlers) ViewSharedPage(c echo.Context) error {
	ctx := c.Request().Context()
	token := c.Param("token")

	// Validate token format
	if len(token) < 40 || len(token) > 50 {
		return c.Render(http.StatusNotFound, "error", map[string]interface{}{
			"error": "Invalid share link",
		})
	}

	// Get share link
	tokenHash := middleware.HashToken(token)
	link, err := h.wikiService.GetDB().GetShareLinkByToken(ctx, tokenHash)
	if err != nil || link == nil {
		return h.renderSharedError(c, "Share link not found", "This share link does not exist or has been deleted.")
	}

	// Validate share link
	if link.IsRevoked {
		return h.renderSharedError(c, "Share link revoked", "This share link has been revoked by the owner.")
	}
	if link.IsExpired() {
		return h.renderSharedError(c, "Share link expired", "This share link has expired.")
	}
	if link.IsViewLimitReached() {
		return h.renderSharedError(c, "View limit reached", "This share link has reached its maximum number of views.")
	}

	// Check IP limit
	if link.MaxIPs != nil {
		ipAddress := sanitizeIP(c.RealIP())
		uniqueIPs, _ := h.wikiService.GetDB().GetShareLinkUniqueIPCount(ctx, link.ID)
		hasAccessed, _ := h.wikiService.GetDB().HasIPAccessedShareLink(ctx, link.ID, ipAddress)
		if !hasAccessed && uniqueIPs >= *link.MaxIPs {
			return h.renderSharedError(c, "Access limit reached", "This share link has reached its maximum number of unique viewers.")
		}
	}

	// Get the optional child slug
	childSlug := c.Param("*")
	var targetSlug string
	var page *models.Page

	if childSlug != "" && link.IncludeChildren {
		// Accessing a child page
		targetSlug = childSlug
		// Verify it's actually a descendant
		isDescendant, err := h.wikiService.GetDB().IsPageDescendant(ctx, link.PageID, childSlug)
		if err != nil || !isDescendant {
			return h.renderSharedError(c, "Page not found", "This page is not accessible via this share link.")
		}
		page, err = h.wikiService.GetPage(ctx, childSlug)
	} else {
		// Accessing the main shared page
		targetSlug = link.PageSlug
		page, err = h.wikiService.GetPage(ctx, link.PageSlug)
	}

	if err != nil || page == nil {
		return h.renderSharedError(c, "Page not found", "The shared page could not be found.")
	}

	// Record the access
	access := &models.ShareLinkAccess{
		ShareLinkID: link.ID,
		IPAddress:   sanitizeIP(c.RealIP()),
		UserAgent:   truncateString(c.Request().UserAgent(), 500),
	}
	_ = h.wikiService.GetDB().RecordShareAccess(ctx, access)
	_ = h.wikiService.GetDB().IncrementShareLinkViewCount(ctx, link.ID)

	// Get child pages if include_children is enabled and we're on the main page
	var childPages []models.PageSummary
	if link.IncludeChildren && targetSlug == link.PageSlug {
		childPages, _ = h.wikiService.GetDB().GetPageChildren(ctx, page.ID)
	}

	// Get TOC
	toc := h.wikiService.GenerateTOC(page.Content)

	data := pages.SharedPageData{
		Page:            page,
		ShareToken:      token,
		IncludeChildren: link.IncludeChildren,
		ChildPages:      childPages,
		TOC:             toc,
		SiteName:        h.config.Site.Name,
		SiteURL:         h.config.Site.URL,
		ParentSlug:      link.PageSlug,
	}

	return pages.SharedPage(data).Render(ctx, c.Response().Writer)
}

// renderSharedError renders an error page for shared access.
func (h *Handlers) renderSharedError(c echo.Context, title, message string) error {
	data := pages.SharedErrorData{
		Title:    title,
		Message:  message,
		SiteName: h.config.Site.Name,
	}
	return pages.SharedError(data).Render(c.Request().Context(), c.Response().Writer)
}

// Helper functions - use middleware versions for consistency

func sanitizeIP(ip string) string {
	return middleware.SanitizeIP(ip)
}

func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}
