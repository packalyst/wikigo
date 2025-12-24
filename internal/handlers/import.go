package handlers

import (
	"bufio"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/services"
	"gowiki/internal/views/pages"
)

// ImportMarkdownForm renders the import form.
func (h *Handlers) ImportMarkdownForm(c echo.Context) error {
	data := pages.ImportData{
		PageData: h.basePageData(c, "Import Markdown"),
	}
	return render(c, http.StatusOK, pages.Import(data))
}

// ImportMarkdown handles markdown file import (supports multiple files).
func (h *Handlers) ImportMarkdown(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil || !user.Role.CanEdit() {
		return echo.NewHTTPError(http.StatusForbidden, "permission denied")
	}

	// Get uploaded files
	form, err := c.MultipartForm()
	if err != nil {
		h.setFlash(c, "error", "Please select markdown files to import")
		return c.Redirect(http.StatusSeeOther, "/import")
	}

	files := form.File["files"]
	if len(files) == 0 {
		h.setFlash(c, "error", "Please select at least one markdown file to import")
		return c.Redirect(http.StatusSeeOther, "/import")
	}

	var imported []string
	var failed []string
	var lastSlug string

	for _, file := range files {
		// Check file extension
		if !strings.HasSuffix(strings.ToLower(file.Filename), ".md") &&
			!strings.HasSuffix(strings.ToLower(file.Filename), ".markdown") {
			failed = append(failed, file.Filename+" (invalid extension)")
			continue
		}

		// Open file
		src, err := file.Open()
		if err != nil {
			failed = append(failed, file.Filename+" (could not open)")
			continue
		}

		// Read content
		content, err := io.ReadAll(src)
		src.Close()
		if err != nil {
			failed = append(failed, file.Filename+" (could not read)")
			continue
		}

		// Parse frontmatter and content
		title, slug, tags, body := parseMarkdownFile(string(content), file.Filename)

		// Check if slug exists
		existingPage, _ := h.wikiService.GetPage(c.Request().Context(), slug)
		if existingPage != nil {
			// If existing page is a placeholder (empty content, auto-created), update it instead
			if strings.TrimSpace(existingPage.Content) == "" {
				// Update the placeholder page with actual content
				update := models.PageUpdate{
					Title:   &title,
					Content: &body,
					Tags:    tags,
				}
				result, err := h.wikiService.UpdatePage(c.Request().Context(), existingPage.ID, user.ID, update, "Imported content")
				if err != nil {
					failed = append(failed, file.Filename+" ("+err.Error()+")")
					continue
				}
				imported = append(imported, title)
				lastSlug = result.Page.Slug
				continue
			}

			// Non-placeholder page exists, append a number to make slug unique
			for i := 2; i < 100; i++ {
				newSlug := slug + "-" + strconv.Itoa(i)
				exists, _ := h.wikiService.PageExists(c.Request().Context(), newSlug)
				if !exists {
					slug = newSlug
					break
				}
			}
		}

		// Create the page
		input := models.PageCreate{
			Title:   title,
			Slug:    slug,
			Content: body,
			Tags:    tags,
		}

		page, err := h.wikiService.CreatePage(c.Request().Context(), user.ID, input)
		if err != nil {
			failed = append(failed, file.Filename+" ("+err.Error()+")")
			continue
		}

		imported = append(imported, title)
		lastSlug = page.Slug
	}

	// Build result message
	if len(imported) > 0 && len(failed) == 0 {
		if len(imported) == 1 {
			h.setFlash(c, "success", "Page imported successfully!")
			return c.Redirect(http.StatusSeeOther, "/wiki/"+lastSlug)
		}
		h.setFlash(c, "success", strconv.Itoa(len(imported))+" pages imported successfully!")
		return c.Redirect(http.StatusSeeOther, "/pages")
	}

	if len(imported) > 0 && len(failed) > 0 {
		h.setFlash(c, "warning", strconv.Itoa(len(imported))+" pages imported, "+strconv.Itoa(len(failed))+" failed: "+strings.Join(failed, ", "))
		return c.Redirect(http.StatusSeeOther, "/pages")
	}

	h.setFlash(c, "error", "Import failed: "+strings.Join(failed, ", "))
	return c.Redirect(http.StatusSeeOther, "/import")
}

// parseMarkdownFile extracts frontmatter and content from a markdown file.
func parseMarkdownFile(content string, filename string) (title, slug string, tags []string, body string) {
	lines := strings.Split(content, "\n")
	body = content

	// Check for YAML frontmatter (starts with ---)
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		// Find closing ---
		endIndex := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				endIndex = i
				break
			}
		}

		if endIndex > 0 {
			// Parse frontmatter
			frontmatter := lines[1:endIndex]
			body = strings.Join(lines[endIndex+1:], "\n")
			body = strings.TrimSpace(body)

			for _, line := range frontmatter {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "title:") {
					title = strings.TrimSpace(strings.TrimPrefix(line, "title:"))
					title = strings.Trim(title, "\"'")
				} else if strings.HasPrefix(line, "slug:") {
					slug = strings.TrimSpace(strings.TrimPrefix(line, "slug:"))
					slug = strings.Trim(slug, "\"'")
				} else if strings.HasPrefix(line, "tags:") {
					tagStr := strings.TrimSpace(strings.TrimPrefix(line, "tags:"))
					tagStr = strings.Trim(tagStr, "[]")
					for _, t := range strings.Split(tagStr, ",") {
						t = strings.TrimSpace(t)
						t = strings.Trim(t, "\"'")
						if t != "" {
							tags = append(tags, t)
						}
					}
				}
			}
		}
	}

	// If no title in frontmatter, try to extract from first heading
	if title == "" {
		scanner := bufio.NewScanner(strings.NewReader(body))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimPrefix(line, "# ")
				break
			}
		}
	}

	// Fallback title from filename
	if title == "" {
		title = strings.TrimSuffix(filename, ".md")
		title = strings.TrimSuffix(title, ".markdown")
		title = strings.ReplaceAll(title, "-", " ")
		title = strings.ReplaceAll(title, "_", " ")
	}

	// Generate slug if not provided
	if slug == "" {
		slug = services.Slugify(title)
	}

	return title, slug, tags, body
}
