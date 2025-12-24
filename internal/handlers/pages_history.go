package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/services"
	"gowiki/internal/views/pages"
)

// PageHistory renders page revision history.
func (h *Handlers) PageHistory(c echo.Context) error {
	slug := c.Param("slug")
	ctx := c.Request().Context()

	page, err := h.wikiService.GetPage(ctx, slug)
	if err != nil {
		if errors.Is(err, services.ErrPageNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Page not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load page")
	}

	revisions, err := h.wikiService.GetPageRevisions(ctx, page.ID, 50, 0)
	if err != nil {
		revisions = []models.RevisionSummary{}
	}

	data := pages.HistoryData{
		PageData:  h.basePageData(c, "History: "+page.Title),
		Page:      page,
		Revisions: revisions,
	}

	return render(c, http.StatusOK, pages.History(data))
}

// ViewRevision renders a specific revision.
func (h *Handlers) ViewRevision(c echo.Context) error {
	revID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid revision ID")
	}

	ctx := c.Request().Context()

	rev, err := h.wikiService.GetRevision(ctx, revID)
	if err != nil {
		if errors.Is(err, services.ErrRevisionNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Revision not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load revision")
	}

	page, err := h.wikiService.GetPageByID(ctx, rev.PageID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to load page")
	}

	contentHTML, err := h.wikiService.RenderMarkdown(rev.Content)
	if err != nil {
		contentHTML = "<p>Failed to render content</p>"
	}

	data := pages.RevisionViewData{
		PageData:    h.basePageData(c, "Revision: "+page.Title),
		Revision:    rev,
		Page:        page,
		ContentHTML: contentHTML,
	}

	return render(c, http.StatusOK, pages.RevisionView(data))
}

// RevertToRevision reverts a page to a previous revision.
func (h *Handlers) RevertToRevision(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Not authenticated")
	}

	revID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid revision ID")
	}

	page, err := h.wikiService.RevertToRevision(c.Request().Context(), revID, user.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to revert")
	}

	h.setFlash(c, "success", "Page reverted to previous version.")

	if c.Request().Header.Get("HX-Request") == "true" {
		c.Response().Header().Set("HX-Redirect", "/wiki/"+page.Slug)
		return c.NoContent(http.StatusOK)
	}

	return c.Redirect(http.StatusSeeOther, "/wiki/"+page.Slug)
}
