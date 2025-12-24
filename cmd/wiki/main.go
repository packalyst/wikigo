package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"

	"gowiki/internal/api"
	"gowiki/internal/config"
	"gowiki/internal/database"
	"gowiki/internal/handlers"
	"gowiki/internal/middleware"
	"gowiki/internal/services"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	fmt.Printf("Starting %s...\n", cfg.Site.Name)

	// Create data directory if needed
	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize database
	db, err := database.New(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Run migrations
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Check if setup is complete
	setupComplete, _ := db.GetSetting(ctx, "setup_complete")
	if setupComplete != "true" {
		fmt.Println("Setup not complete - waiting for admin to complete setup at /setup")
	}

	// Load persisted settings into config
	if requireAuth, _ := db.GetSetting(ctx, "require_auth"); requireAuth == "true" {
		cfg.Site.RequireAuth = true
	}
	if siteName, _ := db.GetSetting(ctx, "site_name"); siteName != "" {
		cfg.Site.Name = siteName
	}
	if allowReg, _ := db.GetSetting(ctx, "allow_registration"); allowReg == "true" {
		cfg.Site.AllowRegistration = true
	}
	if defaultRole, _ := db.GetSetting(ctx, "default_role"); defaultRole != "" {
		cfg.Site.DefaultRole = defaultRole
	}

	// Rebuild FTS index to ensure search works for all existing pages
	if err := db.RebuildFTSIndex(ctx); err != nil {
		fmt.Printf("Warning: Failed to rebuild FTS index: %v\n", err)
	}

	// Initialize services
	markdownService := services.NewMarkdownService()
	authService := services.NewAuthService(db, cfg)
	wikiService := services.NewWikiService(db, markdownService)
	backupService, err := services.NewBackupService(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize backup service: %w", err)
	}

	// Initialize Echo
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Session manager
	sessionManager := middleware.NewSessionManager(cfg, authService)

	// CSRF protection
	csrf := middleware.NewCSRF(sessionManager)

	// Rate limiter
	rateLimiter := middleware.NewRateLimiter(
		cfg.Security.RateLimitRequests,
		cfg.Security.RateLimitWindow,
	)

	// Global middleware (order matters!)
	e.Use(middleware.RequestID())       // Add request ID first for tracing
	e.Use(middleware.RecoveryMiddleware())
	e.Use(middleware.RequestLogger())
	e.Use(middleware.SecurityHeaders())
	e.Use(middleware.SetupRequired(db)) // Redirect to /setup if not complete
	e.Use(rateLimiter.Middleware())
	e.Use(sessionManager.AuthMiddleware())
	e.Use(csrf.Middleware())

	// Gzip compression
	e.Use(echoMiddleware.GzipWithConfig(echoMiddleware.GzipConfig{
		Level: 5,
		Skipper: func(c echo.Context) bool {
			// Skip compression for small responses
			return false
		},
	}))

	// Static files with cache headers
	staticGroup := e.Group("/static")
	staticGroup.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			return next(c)
		}
	})
	staticGroup.Static("/", "static")

	// Uploads (shorter cache since they can change)
	e.Static("/uploads", cfg.Upload.Path)

	// Initialize handlers
	h := handlers.New(cfg, authService, wikiService, backupService, sessionManager)

	// Register routes
	h.RegisterRoutes(e, sessionManager, csrf)

	// Register API routes
	api.RegisterRoutes(e, db, cfg, authService, wikiService)

	// Custom error handler
	e.HTTPErrorHandler = customErrorHandler

	// Start server
	server := &http.Server{
		Addr:         cfg.Address(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Graceful shutdown
	go func() {
		fmt.Printf("Server listening on http://%s\n", cfg.Address())
		if err := e.StartServer(server); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	fmt.Println("Server stopped")
	return nil
}

// customErrorHandler handles HTTP errors.
func customErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	message := "Internal Server Error"

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			message = m
		}
	}

	// For HTMX requests, return minimal HTML
	if c.Request().Header.Get("HX-Request") == "true" {
		c.HTML(code, fmt.Sprintf(`<div class="text-red-600">%s</div>`, message))
		return
	}

	// For API requests, return JSON
	if c.Request().Header.Get("Accept") == "application/json" {
		c.JSON(code, map[string]interface{}{
			"error": message,
			"code":  code,
		})
		return
	}

	// For HTML requests, render error page
	errorHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%d - %s</title>
    <style>
        body { font-family: system-ui, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; background: #f8fafc; }
        .container { text-align: center; padding: 2rem; }
        .code { font-size: 6rem; font-weight: bold; color: #3b82f6; margin: 0; }
        .message { font-size: 1.5rem; color: #475569; margin: 1rem 0; }
        .link { color: #3b82f6; text-decoration: none; }
        .link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <p class="code">%d</p>
        <p class="message">%s</p>
        <a href="/" class="link">← Back to Home</a>
    </div>
</body>
</html>
`, code, message, code, message)

	c.HTML(code, errorHTML)
}
