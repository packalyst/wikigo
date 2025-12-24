package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/models"
	"gowiki/internal/views/pages"
)

// Search handles search queries.
func (h *Handlers) Search(c echo.Context) error {
	query := strings.TrimSpace(c.QueryParam("q"))

	// For HTMX dropdown requests
	if c.Request().Header.Get("HX-Request") == "true" {
		results, _ := h.wikiService.Search(c.Request().Context(), query, 5)
		if results == nil {
			results = []models.SearchResult{}
		}
		return render(c, http.StatusOK, pages.SearchDropdown(results, query))
	}

	// Full search page
	results, _ := h.wikiService.Search(c.Request().Context(), query, 50)
	if results == nil {
		results = []models.SearchResult{}
	}

	data := pages.SearchData{
		PageData: h.basePageData(c, "Search: "+query),
		Query:    query,
		Results:  results,
	}

	return render(c, http.StatusOK, pages.Search(data))
}

// ListTags renders the tags page.
func (h *Handlers) ListTags(c echo.Context) error {
	tags, err := h.wikiService.GetAllTags(c.Request().Context())
	if err != nil {
		tags = []models.Tag{}
	}

	pageData := h.basePageDataWithNav(c, "Tags", "tags")
	pageData.PageTree = h.getPageTree(c)

	data := pages.TagsData{
		PageData: pageData,
		Tags:     tags,
	}

	return render(c, http.StatusOK, pages.Tags(data))
}

// ListPagesByTag renders pages with a specific tag.
func (h *Handlers) ListPagesByTag(c echo.Context) error {
	tag := c.Param("tag")
	pageNum, _ := strconv.Atoi(c.QueryParam("page"))
	if pageNum < 1 {
		pageNum = 1
	}
	perPage := 20

	pageList, err := h.wikiService.GetPagesByTag(c.Request().Context(), tag, perPage, (pageNum-1)*perPage)
	if err != nil {
		pageList = []models.PageSummary{}
	}

	pageData := h.basePageDataWithNav(c, "Tag: "+tag, "tags")
	pageData.PageTree = h.getPageTree(c)

	data := pages.ListData{
		PageData:   pageData,
		Pages:      pageList,
		TotalPages: len(pageList), // Simplified
		Page:       pageNum,
		PerPage:    perPage,
		Tag:        tag,
	}

	return render(c, http.StatusOK, pages.List(data))
}
