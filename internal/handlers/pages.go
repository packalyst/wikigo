package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/services"
	"gowiki/internal/views/pages"
)

// Input length limits
const (
	maxSlugLength    = 200
	maxTitleLength   = 500
	maxContentLength = 1000000 // 1MB
	maxTagLength     = 50
	maxTagsPerPage   = 20
)

// Home renders the home page.
func (h *Handlers) Home(c echo.Context) error {
	ctx := c.Request().Context()

	recentPages, err := h.wikiService.GetRecentPages(ctx, 10)
	if err != nil {
		recentPages = []models.PageSummary{}
	}

	stats, err := h.wikiService.GetStats(ctx)
	if err != nil {
		stats = nil
	}

	var pageStats *pages.WikiStats
	if stats != nil {
		pageStats = &pages.WikiStats{
			PageCount: stats.PageCount,
			UserCount: stats.UserCount,
			TagCount:  stats.TagCount,
		}
	}

	pageData := h.basePageDataWithNav(c, "Home", "home")
	pageData.PageTree = h.getPageTree(c)

	data := pages.HomeData{
		PageData:    pageData,
		RecentPages: recentPages,
		Stats:       pageStats,
	}

	return render(c, http.StatusOK, pages.Home(data))
}

// ViewPage renders a wiki page.
func (h *Handlers) ViewPage(c echo.Context) error {
	slug := c.Param("slug")

	page, err := h.wikiService.GetPage(c.Request().Context(), slug)
	if err != nil {
		if errors.Is(err, services.ErrPageNotFound) {
			// Check if user can edit, offer to create
			user := middleware.GetUser(c)
			if user != nil && user.Role.CanEdit() {
				h.setFlash(c, "info", "Page not found. Would you like to create it?")
				return c.Redirect(http.StatusSeeOther, "/new?slug="+slug)
			}
			return echo.NewHTTPError(http.StatusNotFound, "Page not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load page")
	}

	// Check if page is published or user can view unpublished
	user := middleware.GetUser(c)
	if !page.IsPublished {
		if user == nil || !user.Role.CanEdit() {
			return echo.NewHTTPError(http.StatusNotFound, "Page not found")
		}
	}

	toc := h.wikiService.GenerateTOC(page.Content)

	// Get breadcrumbs (page path)
	ctx := c.Request().Context()
	breadcrumbs, _ := h.wikiService.GetDB().GetPagePath(ctx, page.ID)

	// Get child pages
	children, _ := h.wikiService.GetDB().GetPageChildren(ctx, page.ID)

	pageData := h.basePageDataWithTree(c, page.Title, page.Slug)
	pageData.TOC = toc
	pageData.Breadcrumbs = breadcrumbs

	data := pages.ViewData{
		PageData:    pageData,
		Page:        page,
		TOC:         toc,
		Breadcrumbs: breadcrumbs,
		Children:    children,
	}

	return render(c, http.StatusOK, pages.View(data))
}

// ListPages renders the pages list (only root/top-level pages).
func (h *Handlers) ListPages(c echo.Context) error {
	ctx := c.Request().Context()

	// Get only root pages (parent_id IS NULL)
	pageList, err := h.wikiService.GetDB().GetRootPages(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load pages")
	}

	pageData := h.basePageDataWithNav(c, "All Pages", "pages")
	pageData.PageTree = h.getPageTree(c)

	data := pages.ListData{
		PageData:   pageData,
		Pages:      pageList,
		TotalPages: len(pageList),
		Page:       1,
		PerPage:    len(pageList),
	}

	return render(c, http.StatusOK, pages.List(data))
}

// NewPageForm renders the new page form.
func (h *Handlers) NewPageForm(c echo.Context) error {
	slug := c.QueryParam("slug")

	data := pages.EditData{
		PageData: h.basePageData(c, "New Page"),
		IsNew:    true,
		Errors:   make(map[string]string),
		FormValues: pages.EditFormValues{
			Slug: slug,
		},
	}

	return render(c, http.StatusOK, pages.Edit(data))
}

// CreatePage handles new page creation.
func (h *Handlers) CreatePage(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Not authenticated")
	}

	title := strings.TrimSpace(c.FormValue("title"))
	slug := strings.TrimSpace(c.FormValue("slug"))
	content := c.FormValue("content")
	tagsStr := c.FormValue("tags")

	var tagsList []string
	if tagsStr != "" {
		for _, tag := range strings.Split(tagsStr, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tagsList = append(tagsList, tag)
			}
		}
	}

	errs := make(map[string]string)

	// Validate required fields
	if title == "" {
		errs["title"] = "Title is required."
	}

	// Validate length limits
	if len(title) > maxTitleLength {
		errs["title"] = "Title must be less than 500 characters."
	}
	if len(slug) > maxSlugLength {
		errs["slug"] = "URL slug must be less than 200 characters."
	}
	if len(content) > maxContentLength {
		errs["content"] = "Content is too large (max 1MB)."
	}
	if len(tagsList) > maxTagsPerPage {
		errs["tags"] = "Maximum 20 tags allowed."
	}
	for _, tag := range tagsList {
		if len(tag) > maxTagLength {
			errs["tags"] = "Tag names must be less than 50 characters."
			break
		}
	}

	if len(errs) > 0 {
		data := pages.EditData{
			PageData: h.basePageData(c, "New Page"),
			IsNew:    true,
			Errors:   errs,
			FormValues: pages.EditFormValues{
				Title:   title,
				Slug:    slug,
				Content: content,
				Tags:    tagsStr,
			},
		}
		return render(c, http.StatusBadRequest, pages.Edit(data))
	}

	page, err := h.wikiService.CreatePage(c.Request().Context(), user.ID, models.PageCreate{
		Slug:    slug,
		Title:   title,
		Content: content,
		Tags:    tagsList,
	})

	if err != nil {
		switch {
		case errors.Is(err, services.ErrPageExists):
			errs["slug"] = "A page with this URL already exists."
		case errors.Is(err, services.ErrInvalidSlug):
			errs["slug"] = "Invalid URL slug."
		case errors.Is(err, services.ErrInvalidTitle):
			errs["title"] = "Title is required."
		default:
			errs["title"] = "Failed to create page. Please try again."
		}

		data := pages.EditData{
			PageData: h.basePageData(c, "New Page"),
			IsNew:    true,
			Errors:   errs,
			FormValues: pages.EditFormValues{
				Title:   title,
				Slug:    slug,
				Content: content,
				Tags:    tagsStr,
			},
		}
		return render(c, http.StatusBadRequest, pages.Edit(data))
	}

	// Backup page as markdown file with hierarchical folder structure
	if h.backupService != nil {
		pagePath := getPagePathFromSlug(page.Slug)
		_ = h.backupService.SavePageAsMarkdown(page, user.Username, pagePath)
	}

	h.setFlash(c, "success", "Page created successfully!")
	return c.Redirect(http.StatusSeeOther, "/wiki/"+page.Slug)
}

// EditPageForm renders the edit page form.
func (h *Handlers) EditPageForm(c echo.Context) error {
	slug := c.Param("slug")
	ctx := c.Request().Context()

	page, err := h.wikiService.GetPage(ctx, slug)
	if err != nil {
		if errors.Is(err, services.ErrPageNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Page not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load page")
	}

	// Count all descendant pages for delete warning
	childCount := h.countDescendants(ctx, page.ID)

	data := pages.EditData{
		PageData:   h.basePageData(c, "Edit: "+page.Title),
		Page:       page,
		IsNew:      false,
		Errors:     make(map[string]string),
		ChildCount: childCount,
		FormValues: pages.EditFormValues{
			Slug: page.Slug, // Pre-fill current slug for editing
		},
	}

	return render(c, http.StatusOK, pages.Edit(data))
}

// UpdatePage handles page updates.
func (h *Handlers) UpdatePage(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Not authenticated")
	}

	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid page ID")
	}

	// Get current page to check for slug change
	ctx := c.Request().Context()
	currentPage, err := h.wikiService.GetPageByID(ctx, pageID)
	if err != nil {
		if errors.Is(err, services.ErrPageNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Page not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load page")
	}
	oldSlug := currentPage.Slug

	title := strings.TrimSpace(c.FormValue("title"))
	slug := strings.TrimSpace(c.FormValue("slug"))
	content := c.FormValue("content")
	tagsStr := c.FormValue("tags")

	var tagsList []string
	if tagsStr != "" {
		for _, tag := range strings.Split(tagsStr, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tagsList = append(tagsList, tag)
			}
		}
	}

	// Validate length limits
	if len(title) > maxTitleLength {
		return echo.NewHTTPError(http.StatusBadRequest, "Title must be less than 500 characters")
	}
	if len(slug) > maxSlugLength {
		return echo.NewHTTPError(http.StatusBadRequest, "URL slug must be less than 200 characters")
	}
	if len(content) > maxContentLength {
		return echo.NewHTTPError(http.StatusBadRequest, "Content is too large (max 1MB)")
	}
	if len(tagsList) > maxTagsPerPage {
		return echo.NewHTTPError(http.StatusBadRequest, "Maximum 20 tags allowed")
	}
	for _, tag := range tagsList {
		if len(tag) > maxTagLength {
			return echo.NewHTTPError(http.StatusBadRequest, "Tag names must be less than 50 characters")
		}
	}

	// Build update with slug if provided
	update := models.PageUpdate{
		Title:   &title,
		Content: &content,
		Tags:    tagsList,
	}
	if slug != "" {
		update.Slug = &slug
	}

	result, err := h.wikiService.UpdatePage(ctx, pageID, user.ID, update, "Updated via web editor")

	if err != nil {
		if errors.Is(err, services.ErrPageNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Page not found")
		}
		if errors.Is(err, services.ErrPageExists) {
			return echo.NewHTTPError(http.StatusBadRequest, "A page with this URL already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update page")
	}

	page := result.Page

	// Handle backup: delete old if slug changed, save new
	if h.backupService != nil {
		if oldSlug != page.Slug {
			// Delete old backup at old path
			oldPath := getPagePathFromSlug(oldSlug)
			_ = h.backupService.DeleteBackup(oldSlug, oldPath)
		}
		// Save at new path
		pagePath := getPagePathFromSlug(page.Slug)
		_ = h.backupService.SavePageAsMarkdown(page, user.Username, pagePath)

		// Handle cascaded slug changes for child pages
		for _, change := range result.SlugChanges {
			// Delete old backup
			oldPath := getPagePathFromSlug(change.OldSlug)
			_ = h.backupService.DeleteBackup(change.OldSlug, oldPath)

			// Create new backup at new path (we need to fetch the page to get full content)
			childPage, err := h.wikiService.GetPage(ctx, change.NewSlug)
			if err == nil && childPage != nil {
				newPath := getPagePathFromSlug(change.NewSlug)
				_ = h.backupService.SavePageAsMarkdown(childPage, user.Username, newPath)
			}
		}
	}

	h.setFlash(c, "success", "Page updated successfully!")
	return c.Redirect(http.StatusSeeOther, "/wiki/"+page.Slug)
}

// DeletePage handles page deletion with cascade delete for child pages.
func (h *Handlers) DeletePage(c echo.Context) error {
	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid page ID")
	}

	ctx := c.Request().Context()
	db := h.wikiService.GetDB()

	// Get the page to delete
	page, err := h.wikiService.GetPageByID(ctx, pageID)
	if err != nil {
		if errors.Is(err, services.ErrPageNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Page not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load page")
	}

	// Collect all pages to delete (this page + all descendants)
	pagesToDelete := []pageInfo{{ID: page.ID, Slug: page.Slug}}
	if err := h.collectDescendants(ctx, page.ID, &pagesToDelete); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to collect child pages")
	}

	// Build list of IDs in reverse order (children first, parents last)
	pageIDs := make([]int64, len(pagesToDelete))
	for i := len(pagesToDelete) - 1; i >= 0; i-- {
		pageIDs[len(pagesToDelete)-1-i] = pagesToDelete[i].ID
	}

	// Delete all pages in a single transaction
	if err := db.DeletePages(ctx, pageIDs); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete pages")
	}

	// Delete backup files (after successful database deletion)
	if h.backupService != nil {
		for _, p := range pagesToDelete {
			pagePath := getPagePathFromSlug(p.Slug)
			_ = h.backupService.DeleteBackup(p.Slug, pagePath)
		}
	}

	// Build flash message
	msg := "Page deleted successfully."
	if len(pagesToDelete) > 1 {
		msg = "Page and " + strconv.Itoa(len(pagesToDelete)-1) + " child page(s) deleted successfully."
	}
	h.setFlash(c, "success", msg)

	// For HTMX requests, redirect via header
	if c.Request().Header.Get("HX-Request") == "true" {
		c.Response().Header().Set("HX-Redirect", "/")
		return c.NoContent(http.StatusOK)
	}

	return c.Redirect(http.StatusSeeOther, "/")
}
