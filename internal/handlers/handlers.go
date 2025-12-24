package handlers

import (
	"github.com/labstack/echo/v4"

	"gowiki/internal/config"
	"gowiki/internal/database"
	"gowiki/internal/middleware"
	"gowiki/internal/models"
	"gowiki/internal/services"
	"gowiki/internal/views/layouts"
)

// Handlers contains all HTTP request handlers.
type Handlers struct {
	config         *config.Config
	authService    *services.AuthService
	wikiService    *services.WikiService
	backupService  *services.BackupService
	sessionManager *middleware.SessionManager
	loginLimiter   *middleware.LoginRateLimiter
}

// New creates a new Handlers instance.
func New(
	cfg *config.Config,
	authService *services.AuthService,
	wikiService *services.WikiService,
	backupService *services.BackupService,
	sessionManager *middleware.SessionManager,
) *Handlers {
	return &Handlers{
		config:         cfg,
		authService:    authService,
		wikiService:    wikiService,
		backupService:  backupService,
		sessionManager: sessionManager,
		loginLimiter:   middleware.NewLoginRateLimiter(cfg.Security.LoginMaxAttempts, cfg.Security.LoginLockoutTime),
	}
}

// basePageData creates the common page data structure.
func (h *Handlers) basePageData(c echo.Context, title string) layouts.PageData {
	return h.basePageDataWithNav(c, title, "")
}

// basePageDataWithNav creates page data with active navigation set.
func (h *Handlers) basePageDataWithNav(c echo.Context, title, activeNav string) layouts.PageData {
	user := middleware.GetUser(c)
	csrfToken := middleware.GetCSRFToken(c)

	flash := layouts.FlashMessages{
		Success: h.sessionManager.GetFlash(c, "success"),
		Error:   h.sessionManager.GetFlash(c, "error"),
		Info:    h.sessionManager.GetFlash(c, "info"),
	}

	return layouts.PageData{
		Title:       title,
		SiteName:    h.config.Site.Name,
		Description: h.config.Site.Name + " - A collaborative wiki",
		User:        user,
		CSRFToken:   csrfToken,
		Flash:       flash,
		ActiveNav:   activeNav,
	}
}

// setFlash sets a flash message.
func (h *Handlers) setFlash(c echo.Context, key, message string) {
	h.sessionManager.SetFlash(c, key, message)
}

// basePageDataWithTree creates page data with page tree for sidebar navigation.
func (h *Handlers) basePageDataWithTree(c echo.Context, title, currentSlug string) layouts.PageData {
	data := h.basePageData(c, title)

	// Get page tree for sidebar
	ctx := c.Request().Context()
	tree, err := h.wikiService.GetDB().GetPageTree(ctx)
	if err == nil {
		data.PageTree = tree
	}
	data.CurrentSlug = currentSlug

	return data
}

// getPageTree returns the page tree for navigation.
func (h *Handlers) getPageTree(c echo.Context) []*database.PageTreeNode {
	ctx := c.Request().Context()
	tree, _ := h.wikiService.GetDB().GetPageTree(ctx)
	return tree
}

// RegisterRoutes registers all HTTP routes.
func (h *Handlers) RegisterRoutes(e *echo.Echo, sm *middleware.SessionManager, csrf *middleware.CSRF) {
	// Setup routes (no auth, no CSRF)
	e.GET("/setup", h.SetupPage)
	e.POST("/setup", h.SetupSubmit)

	// Health check (always public)
	e.GET("/health", h.HealthCheck)

	// Public routes (may require auth if private wiki mode is enabled)
	publicGroup := e.Group("")
	publicGroup.Use(middleware.RequireAuthIfPrivate(h.config))
	publicGroup.GET("/", h.Home)
	publicGroup.GET("/wiki/:slug", h.ViewPage)
	publicGroup.GET("/pages", h.ListPages)
	publicGroup.GET("/tags", h.ListTags)
	publicGroup.GET("/tag/:tag", h.ListPagesByTag)
	publicGroup.GET("/search", h.Search)

	// Auth routes (no auth required)
	authGroup := e.Group("")
	authGroup.Use(middleware.RequireNoAuth())
	authGroup.GET("/login", h.LoginForm)
	authGroup.POST("/login", h.Login)
	// Always register routes - handler checks if registration is allowed
	authGroup.GET("/register", h.RegisterForm)
	authGroup.POST("/register", h.Register)

	// Logout (requires auth)
	e.POST("/logout", h.Logout, middleware.RequireAuth())

	// User routes (requires auth)
	userGroup := e.Group("")
	userGroup.Use(middleware.RequireAuth())
	userGroup.GET("/tokens", h.TokensPage)
	userGroup.POST("/tokens", h.CreateToken)
	userGroup.DELETE("/tokens/:id", h.DeleteToken)

	// Editor routes (requires editor role)
	editorGroup := e.Group("")
	editorGroup.Use(middleware.RequireRole(models.RoleEditor))
	editorGroup.GET("/new", h.NewPageForm)
	editorGroup.POST("/pages", h.CreatePage)
	editorGroup.GET("/edit/:slug", h.EditPageForm)
	editorGroup.POST("/pages/:id", h.UpdatePage)
	editorGroup.DELETE("/pages/:id", h.DeletePage)
	editorGroup.GET("/history/:slug", h.PageHistory)
	editorGroup.GET("/revision/:id", h.ViewRevision)
	editorGroup.POST("/revert/:id", h.RevertToRevision)
	editorGroup.POST("/preview", h.PreviewMarkdown)
	editorGroup.POST("/upload", h.UploadFile)
	editorGroup.GET("/import", h.ImportMarkdownForm)
	editorGroup.POST("/import", h.ImportMarkdown)

	// Admin routes
	adminGroup := e.Group("/admin")
	adminGroup.Use(middleware.RequireRole(models.RoleAdmin))
	adminGroup.GET("", h.AdminDashboard)
	adminGroup.GET("/users", h.AdminListUsers)
	adminGroup.POST("/users", h.AdminCreateUser)
	adminGroup.POST("/users/:id", h.AdminUpdateUser)
	adminGroup.DELETE("/users/:id", h.AdminDeleteUser)
	adminGroup.POST("/settings", h.AdminUpdateSettings)
}
