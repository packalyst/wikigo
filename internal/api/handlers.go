package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"gowiki/internal/config"
	"gowiki/internal/database"
	"gowiki/internal/models"
	"gowiki/internal/services"
)

// Handlers contains all API request handlers.
type Handlers struct {
	db          *database.DB
	config      *config.Config
	authService *services.AuthService
	wikiService *services.WikiService
}

// NewHandlers creates a new API handlers instance.
func NewHandlers(
	db *database.DB,
	cfg *config.Config,
	authService *services.AuthService,
	wikiService *services.WikiService,
) *Handlers {
	return &Handlers{
		db:          db,
		config:      cfg,
		authService: authService,
		wikiService: wikiService,
	}
}

// Response helpers

type successResponse struct {
	Data interface{} `json:"data"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Details string `json:"details,omitempty"`
}

type paginatedResponse struct {
	Data   interface{} `json:"data"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

func success(c echo.Context, data interface{}) error {
	return c.JSON(http.StatusOK, successResponse{Data: data})
}

func created(c echo.Context, data interface{}) error {
	return c.JSON(http.StatusCreated, successResponse{Data: data})
}

func paginated(c echo.Context, data interface{}, total, limit, offset int) error {
	return c.JSON(http.StatusOK, paginatedResponse{
		Data:   data,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// Auth handlers

// LoginRequest represents a login request.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// TokenResponse represents the response with access and refresh tokens.
type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Login authenticates a user and returns JWT tokens.
func (h *Handlers) Login(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.Username == "" || req.Password == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "username and password are required")
	}

	// Authenticate user
	user, err := h.authService.Authenticate(c.Request().Context(), req.Username, req.Password)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}

	// Generate access token
	accessToken, err := GenerateJWT(user, h.config.Security.SecretKey, h.config.Security.JWTAccessExpiry)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}

	// Generate refresh token (longer lived)
	refreshToken, err := GenerateJWT(user, h.config.Security.SecretKey, h.config.Security.JWTRefreshExpiry)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate refresh token")
	}

	// Update last login
	h.db.UpdateUserLastLogin(c.Request().Context(), user.ID)

	return c.JSON(http.StatusOK, TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(h.config.Security.JWTAccessExpiry),
	})
}

// RefreshToken refreshes an access token using a refresh token.
func (h *Handlers) RefreshToken(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	// Generate new access token
	accessToken, err := GenerateJWT(user, h.config.Security.SecretKey, h.config.Security.JWTAccessExpiry)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}

	return c.JSON(http.StatusOK, TokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(h.config.Security.JWTAccessExpiry),
	})
}

// Page handlers

// ListPages returns a paginated list of pages.
func (h *Handlers) ListPages(c echo.Context) error {
	filter := models.NewPageFilter()

	if limit := c.QueryParam("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 100 {
			filter.Limit = l
		}
	}
	if offset := c.QueryParam("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil && o >= 0 {
			filter.Offset = o
		}
	}
	if tag := c.QueryParam("tag"); tag != "" {
		filter.Tag = &tag
	}
	if orderBy := c.QueryParam("order_by"); orderBy != "" {
		filter.OrderBy = orderBy
	}
	if orderDir := c.QueryParam("order_dir"); orderDir != "" {
		filter.OrderDir = orderDir
	}

	// Only show published pages for non-editors
	user := GetAPIUser(c)
	if user == nil || !user.Role.CanEdit() {
		published := true
		filter.IsPublished = &published
	}

	pages, err := h.db.ListPages(c.Request().Context(), filter)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list pages")
	}

	total, _ := h.db.CountPages(c.Request().Context())

	return paginated(c, pages, total, filter.Limit, filter.Offset)
}

// GetPage returns a single page by slug.
func (h *Handlers) GetPage(c echo.Context) error {
	slug := c.Param("slug")
	if slug == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "slug is required")
	}

	page, err := h.db.GetPageBySlug(c.Request().Context(), slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get page")
	}
	if page == nil {
		return echo.NewHTTPError(http.StatusNotFound, "page not found")
	}

	// Check if user can view unpublished pages
	user := GetAPIUser(c)
	if !page.IsPublished && (user == nil || !user.Role.CanEdit()) {
		return echo.NewHTTPError(http.StatusNotFound, "page not found")
	}

	return success(c, page)
}

// CreatePageRequest represents a request to create a page.
type CreatePageRequest struct {
	Title   string   `json:"title"`
	Slug    string   `json:"slug"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// CreatePage creates a new page.
func (h *Handlers) CreatePage(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil || !user.Role.CanEdit() {
		return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions")
	}

	var req CreatePageRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.Title == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "title is required")
	}

	// Generate slug if not provided
	slug := req.Slug
	if slug == "" {
		slug = services.Slugify(req.Title)
	}

	// Check if slug exists
	existing, _ := h.db.GetPageBySlug(c.Request().Context(), slug)
	if existing != nil {
		return echo.NewHTTPError(http.StatusConflict, "page with this slug already exists")
	}

	// Render content
	html, err := h.wikiService.RenderMarkdown(req.Content)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to render content")
	}

	page := &models.Page{
		Slug:        slug,
		Title:       req.Title,
		Content:     req.Content,
		ContentHTML: html,
		AuthorID:    user.ID,
		IsPublished: true,
	}

	if err := h.db.CreatePage(c.Request().Context(), page); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create page")
	}

	// Set tags
	if len(req.Tags) > 0 {
		if err := h.db.SetPageTags(c.Request().Context(), page.ID, req.Tags); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to set tags")
		}
	}

	// Reload page with tags
	page, _ = h.db.GetPageByID(c.Request().Context(), page.ID)

	return created(c, page)
}

// UpdatePageRequest represents a request to update a page.
type UpdatePageRequest struct {
	Title       *string  `json:"title"`
	Content     *string  `json:"content"`
	Tags        []string `json:"tags"`
	IsPublished *bool    `json:"is_published"`
}

// UpdatePage updates an existing page.
func (h *Handlers) UpdatePage(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil || !user.Role.CanEdit() {
		return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions")
	}

	slug := c.Param("slug")
	page, err := h.db.GetPageBySlug(c.Request().Context(), slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get page")
	}
	if page == nil {
		return echo.NewHTTPError(http.StatusNotFound, "page not found")
	}

	var req UpdatePageRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Create revision before updating
	revision := &models.Revision{
		PageID:   page.ID,
		Content:  page.Content,
		AuthorID: user.ID,
		Comment:  "API update",
	}
	h.db.CreateRevision(c.Request().Context(), revision)

	// Update fields
	if req.Title != nil {
		page.Title = *req.Title
	}
	if req.Content != nil {
		page.Content = *req.Content
		html, err := h.wikiService.RenderMarkdown(page.Content)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to render content")
		}
		page.ContentHTML = html
	}
	if req.IsPublished != nil {
		page.IsPublished = *req.IsPublished
	}

	if err := h.db.UpdatePage(c.Request().Context(), page); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update page")
	}

	// Update tags if provided
	if req.Tags != nil {
		if err := h.db.SetPageTags(c.Request().Context(), page.ID, req.Tags); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to set tags")
		}
	}

	// Reload page with tags
	page, _ = h.db.GetPageBySlug(c.Request().Context(), slug)

	return success(c, page)
}

// DeletePage deletes a page.
func (h *Handlers) DeletePage(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil || !user.Role.CanEdit() {
		return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions")
	}

	slug := c.Param("slug")
	page, err := h.db.GetPageBySlug(c.Request().Context(), slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get page")
	}
	if page == nil {
		return echo.NewHTTPError(http.StatusNotFound, "page not found")
	}

	if err := h.db.DeletePage(c.Request().Context(), page.ID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete page")
	}

	return c.NoContent(http.StatusNoContent)
}

// Tag handlers

// ListTags returns all tags.
func (h *Handlers) ListTags(c echo.Context) error {
	tags, err := h.db.ListTags(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list tags")
	}

	return success(c, tags)
}

// GetTagPages returns pages for a specific tag.
func (h *Handlers) GetTagPages(c echo.Context) error {
	tagName := c.Param("name")
	if tagName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "tag name is required")
	}

	filter := models.NewPageFilter()
	filter.Tag = &tagName

	// Only show published pages for non-editors
	user := GetAPIUser(c)
	if user == nil || !user.Role.CanEdit() {
		published := true
		filter.IsPublished = &published
	}

	pages, err := h.db.ListPages(c.Request().Context(), filter)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list pages")
	}

	return success(c, pages)
}

// Search handlers

// Search performs a full-text search.
func (h *Handlers) Search(c echo.Context) error {
	query := c.QueryParam("q")
	if query == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "search query is required")
	}

	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	results, err := h.db.SearchPages(c.Request().Context(), query, limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "search failed")
	}

	return success(c, results)
}

// User handlers (admin only)

// ListUsers returns all users (admin only).
func (h *Handlers) ListUsers(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil || !user.Role.CanAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "admin access required")
	}

	users, err := h.db.ListUsers(c.Request().Context(), 100, 0)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list users")
	}

	return success(c, users)
}

// GetCurrentUser returns the current authenticated user.
func (h *Handlers) GetCurrentUser(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	return success(c, user)
}

// API Token handlers

// CreateAPITokenRequest represents a request to create an API token.
type CreateAPITokenRequest struct {
	Name   string `json:"name"`
	Scopes string `json:"scopes"`
}

// CreateAPITokenResponse includes the raw token (only shown once).
type CreateAPITokenResponse struct {
	Token     string           `json:"token"` // Raw token, only shown once
	TokenInfo *models.APIToken `json:"token_info"`
}

// CreateAPIToken creates a new API token for the current user.
func (h *Handlers) CreateAPIToken(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	var req CreateAPITokenRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}
	rawToken := hex.EncodeToString(tokenBytes)
	tokenHash := HashToken(rawToken)

	// Default scopes
	scopes := req.Scopes
	if scopes == "" {
		scopes = "read"
	}

	token := &models.APIToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		Name:      req.Name,
		Scopes:    scopes,
		ExpiresAt: time.Now().Add(h.config.Security.JWTRefreshExpiry),
	}

	if err := h.db.CreateAPIToken(c.Request().Context(), token); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create token")
	}

	return created(c, CreateAPITokenResponse{
		Token:     rawToken,
		TokenInfo: token,
	})
}

// ListAPITokens returns all tokens for the current user.
func (h *Handlers) ListAPITokens(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	tokens, err := h.db.ListAPITokensByUser(c.Request().Context(), user.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list tokens")
	}

	return success(c, tokens)
}

// DeleteAPIToken deletes an API token.
func (h *Handlers) DeleteAPIToken(c echo.Context) error {
	user := GetAPIUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid token ID")
	}

	// Verify token belongs to user (unless admin)
	tokens, _ := h.db.ListAPITokensByUser(c.Request().Context(), user.ID)
	found := false
	for _, t := range tokens {
		if t.ID == tokenID {
			found = true
			break
		}
	}

	if !found && !user.Role.CanAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "token not found or not owned by user")
	}

	if err := h.db.DeleteAPIToken(c.Request().Context(), tokenID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete token")
	}

	return c.NoContent(http.StatusNoContent)
}
