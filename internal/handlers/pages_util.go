package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// PreviewMarkdown renders markdown preview.
func (h *Handlers) PreviewMarkdown(c echo.Context) error {
	content := c.FormValue("content")

	html, err := h.wikiService.RenderMarkdown(content)
	if err != nil {
		return c.HTML(http.StatusOK, "<p class='text-red-500'>Failed to render markdown</p>")
	}

	return c.HTML(http.StatusOK, html)
}

// HealthCheck returns server health status.
func (h *Handlers) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// pageInfo holds basic page info for deletion.
type pageInfo struct {
	ID   int64
	Slug string
}

// countDescendants counts all descendant pages recursively.
func (h *Handlers) countDescendants(ctx context.Context, parentID int64) int {
	children, err := h.wikiService.GetDB().GetPageChildren(ctx, parentID)
	if err != nil {
		return 0
	}

	count := len(children)
	for _, child := range children {
		count += h.countDescendants(ctx, child.ID)
	}
	return count
}

// collectDescendants recursively collects all descendant pages.
func (h *Handlers) collectDescendants(ctx context.Context, parentID int64, pages *[]pageInfo) error {
	children, err := h.wikiService.GetDB().GetPageChildren(ctx, parentID)
	if err != nil {
		return err
	}

	for _, child := range children {
		*pages = append(*pages, pageInfo{ID: child.ID, Slug: child.Slug})
		if err := h.collectDescendants(ctx, child.ID, pages); err != nil {
			return err
		}
	}

	return nil
}

// getPagePathFromSlug extracts parent path segments from a slug.
// For "linux/ubuntu/networking", returns ["linux", "ubuntu"].
// For "simple-page", returns empty slice.
func getPagePathFromSlug(slug string) []string {
	parts := strings.Split(slug, "/")
	if len(parts) <= 1 {
		return []string{}
	}
	return parts[:len(parts)-1]
}
