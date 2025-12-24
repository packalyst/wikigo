package api

import (
	"github.com/labstack/echo/v4"

	"gowiki/internal/config"
	"gowiki/internal/database"
	"gowiki/internal/models"
	"gowiki/internal/services"
)

// RegisterRoutes registers all API routes.
func RegisterRoutes(
	e *echo.Echo,
	db *database.DB,
	cfg *config.Config,
	authService *services.AuthService,
	wikiService *services.WikiService,
) {
	// Create handlers and middleware
	h := NewHandlers(db, cfg, authService, wikiService)
	jwtMiddleware := NewJWTMiddleware(db, cfg)

	// API group
	api := e.Group("/api/v1")

	// Public routes (no auth required)
	api.POST("/auth/login", h.Login)

	// Routes with optional auth
	optionalAuth := api.Group("")
	optionalAuth.Use(jwtMiddleware.OptionalMiddleware())
	optionalAuth.GET("/pages", h.ListPages)
	optionalAuth.GET("/pages/:slug", h.GetPage)
	optionalAuth.GET("/tags", h.ListTags)
	optionalAuth.GET("/tags/:name", h.GetTagPages)
	optionalAuth.GET("/search", h.Search)

	// Protected routes (auth required)
	protected := api.Group("")
	protected.Use(jwtMiddleware.Middleware())

	// Token refresh
	protected.POST("/auth/refresh", h.RefreshToken)

	// Current user
	protected.GET("/me", h.GetCurrentUser)

	// API tokens management
	protected.POST("/tokens", h.CreateAPIToken)
	protected.GET("/tokens", h.ListAPITokens)
	protected.DELETE("/tokens/:id", h.DeleteAPIToken)

	// Editor routes
	editor := protected.Group("")
	editor.Use(RequireRole(models.RoleEditor))
	editor.POST("/pages", h.CreatePage)
	editor.PUT("/pages/:slug", h.UpdatePage)
	editor.DELETE("/pages/:slug", h.DeletePage)

	// Admin routes
	admin := protected.Group("/admin")
	admin.Use(RequireRole(models.RoleAdmin))
	admin.GET("/users", h.ListUsers)
}
