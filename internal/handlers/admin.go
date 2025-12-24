package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/views/admin"
)

// AdminDashboard renders the admin dashboard.
func (h *Handlers) AdminDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	stats, _ := h.wikiService.GetStats(ctx)
	users, _ := h.authService.ListUsers(ctx, 100, 0)

	data := admin.DashboardData{
		PageData: h.basePageData(c, "Admin Dashboard"),
		Users:    users,
		Settings: &admin.Settings{
			SiteName:          h.config.Site.Name,
			AllowRegistration: h.config.Site.AllowRegistration,
			DefaultRole:       h.config.Site.DefaultRole,
			RequireAuth:       h.config.Site.RequireAuth,
		},
	}

	if stats != nil {
		data.Stats = &admin.Stats{
			PageCount: stats.PageCount,
			UserCount: stats.UserCount,
			TagCount:  stats.TagCount,
		}
	} else {
		data.Stats = &admin.Stats{}
	}

	return render(c, http.StatusOK, admin.Dashboard(data))
}

// AdminListUsers returns the user list for admin.
func (h *Handlers) AdminListUsers(c echo.Context) error {
	pageNum, _ := strconv.Atoi(c.QueryParam("page"))
	if pageNum < 1 {
		pageNum = 1
	}
	perPage := 20

	users, err := h.authService.ListUsers(c.Request().Context(), perPage, (pageNum-1)*perPage)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load users")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"users": users,
		"page":  pageNum,
	})
}

// AdminCreateUser creates a new user.
func (h *Handlers) AdminCreateUser(c echo.Context) error {
	username := strings.TrimSpace(c.FormValue("username"))
	email := strings.TrimSpace(c.FormValue("email"))
	password := c.FormValue("password")
	role := models.Role(c.FormValue("role"))

	if !role.IsValid() {
		role = models.RoleViewer
	}

	ctx := c.Request().Context()
	newUser, err := h.authService.CreateUser(ctx, models.UserCreate{
		Username: username,
		Email:    email,
		Password: password,
		Role:     role,
	})

	// Return JSON for AJAX requests
	if c.Request().Header.Get("X-CSRF-Token") != "" {
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		}
		// Audit log: user created
		h.logAdminAction(c, "user_create", "user", &newUser.ID, map[string]interface{}{
			"username": username,
			"role":     string(role),
		})
		return c.JSON(http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "User created successfully",
		})
	}

	// Regular form submission
	if err != nil {
		h.setFlash(c, "error", err.Error())
		return c.Redirect(http.StatusSeeOther, "/admin")
	}

	// Audit log: user created
	h.logAdminAction(c, "user_create", "user", &newUser.ID, map[string]interface{}{
		"username": username,
		"role":     string(role),
	})

	h.setFlash(c, "success", "User created successfully")
	return c.Redirect(http.StatusSeeOther, "/admin")
}

// AdminUpdateUser updates a user.
func (h *Handlers) AdminUpdateUser(c echo.Context) error {
	isAjax := c.Request().Header.Get("X-CSRF-Token") != ""

	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		if isAjax {
			return c.JSON(http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Invalid user ID",
			})
		}
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid user ID")
	}

	update := &models.UserUpdate{}

	if email := c.FormValue("email"); email != "" {
		update.Email = &email
	}

	if password := c.FormValue("password"); password != "" {
		update.Password = &password
	}

	if roleStr := c.FormValue("role"); roleStr != "" {
		role := models.Role(roleStr)
		if role.IsValid() {
			update.Role = &role
		}
	}

	if isActiveStr := c.FormValue("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true" || isActiveStr == "1"
		update.IsActive = &isActive
	} else {
		// Checkbox not checked means inactive
		isActive := false
		update.IsActive = &isActive
	}

	if err := h.authService.UpdateUser(c.Request().Context(), userID, update); err != nil {
		if isAjax {
			return c.JSON(http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		}
		h.setFlash(c, "error", err.Error())
		return c.Redirect(http.StatusSeeOther, "/admin")
	}

	// Audit log: user updated
	h.logAdminAction(c, "user_update", "user", &userID, map[string]interface{}{
		"fields_updated": update,
	})

	if isAjax {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "User updated successfully",
		})
	}

	h.setFlash(c, "success", "User updated successfully")
	return c.Redirect(http.StatusSeeOther, "/admin")
}

// AdminDeleteUser deletes a user.
func (h *Handlers) AdminDeleteUser(c echo.Context) error {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid user ID")
	}

	if err := h.authService.DeleteUser(c.Request().Context(), userID); err != nil {
		c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"Failed to delete user","type":"error"}}`)
		return c.NoContent(http.StatusInternalServerError)
	}

	// Audit log: user deleted
	h.logAdminAction(c, "user_delete", "user", &userID, nil)

	// Return empty response for HTMX to remove the row + toast
	c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"User deleted successfully","type":"success"}}`)
	return c.NoContent(http.StatusOK)
}

// AdminUpdateSettings updates wiki settings.
func (h *Handlers) AdminUpdateSettings(c echo.Context) error {
	ctx := c.Request().Context()

	siteName := strings.TrimSpace(c.FormValue("site_name"))
	allowReg := c.FormValue("allow_registration") == "true"
	requireAuth := c.FormValue("require_auth") == "true"
	defaultRole := c.FormValue("default_role")

	// Update config in memory
	if siteName != "" {
		h.config.Site.Name = siteName
	}
	h.config.Site.AllowRegistration = allowReg
	h.config.Site.RequireAuth = requireAuth
	if defaultRole == "viewer" || defaultRole == "editor" {
		h.config.Site.DefaultRole = defaultRole
	}

	// Persist settings to database
	if siteName != "" {
		h.authService.SetSetting(ctx, "site_name", siteName)
	}
	h.authService.SetSetting(ctx, "allow_registration", strconv.FormatBool(allowReg))
	h.authService.SetSetting(ctx, "require_auth", strconv.FormatBool(requireAuth))
	if defaultRole == "viewer" || defaultRole == "editor" {
		h.authService.SetSetting(ctx, "default_role", defaultRole)
	}

	// Audit log: settings updated
	h.logAdminAction(c, "settings_update", "settings", nil, map[string]interface{}{
		"site_name":          siteName,
		"allow_registration": allowReg,
		"require_auth":       requireAuth,
		"default_role":       defaultRole,
	})

	// Check if this is an HTMX request
	if c.Request().Header.Get("HX-Request") == "true" {
		c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"Settings updated successfully","type":"success"}}`)
		return c.NoContent(http.StatusOK)
	}

	h.setFlash(c, "success", "Settings updated successfully")
	return c.Redirect(http.StatusSeeOther, "/admin")
}

// AdminGenerateBackups generates markdown backup files for all wiki pages.
func (h *Handlers) AdminGenerateBackups(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil || user.Role != models.RoleAdmin {
		return echo.NewHTTPError(http.StatusForbidden, "Admin access required")
	}

	if h.backupService == nil {
		c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"Backup service not configured","type":"error"}}`)
		return c.NoContent(http.StatusInternalServerError)
	}

	ctx := c.Request().Context()

	// Get all pages
	filter := models.NewPageFilter()
	filter.Limit = 10000 // Get all pages
	pages, err := h.wikiService.ListPages(ctx, filter)
	if err != nil {
		c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"Failed to load pages","type":"error"}}`)
		return c.NoContent(http.StatusInternalServerError)
	}

	// Generate backup for each page
	var successCount, errorCount int
	for _, pageSummary := range pages {
		// Get full page content
		page, err := h.wikiService.GetPage(ctx, pageSummary.Slug)
		if err != nil {
			errorCount++
			continue
		}

		// Generate backup
		pagePath := getPagePathFromSlug(page.Slug)
		if err := h.backupService.SavePageAsMarkdown(page, user.Username, pagePath); err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	// Audit log
	h.logAdminAction(c, "generate_backups", "system", nil, map[string]interface{}{
		"success_count": successCount,
		"error_count":   errorCount,
	})

	// Return result
	message := "Generated " + strconv.Itoa(successCount) + " backup files"
	if errorCount > 0 {
		message += " (" + strconv.Itoa(errorCount) + " errors)"
	}
	c.Response().Header().Set("HX-Trigger", `{"showToast":{"message":"`+message+`","type":"success"}}`)
	return c.NoContent(http.StatusOK)
}

// logAdminAction logs an admin action to the audit log.
func (h *Handlers) logAdminAction(c echo.Context, action, entityType string, entityID *int64, details map[string]interface{}) {
	user := middleware.GetUser(c)
	if user == nil {
		return
	}

	var detailsStr string
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			detailsStr = string(b)
		}
	}

	// Fire and forget - don't block on audit logging
	// Use background context since the request context may be cancelled
	ipAddr := c.RealIP()
	go func() {
		_ = h.wikiService.GetDB().LogAudit(
			context.Background(),
			&user.ID,
			action,
			entityType,
			entityID,
			detailsStr,
			ipAddr,
		)
	}()
}
