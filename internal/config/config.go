package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Security SecurityConfig
	Site     SiteConfig
	Upload   UploadConfig
	Backup   BackupConfig
}

// BackupConfig contains markdown backup settings.
type BackupConfig struct {
	Enabled bool
	Path    string
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Port            int
	Host            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

// DatabaseConfig contains database connection settings.
type DatabaseConfig struct {
	Path            string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// SecurityConfig contains security-related settings.
type SecurityConfig struct {
	SecretKey           string
	SessionName         string
	SessionMaxAge       int
	CSRFTokenLength     int
	BcryptCost          int
	RateLimitRequests   int
	RateLimitWindow     time.Duration
	JWTAccessExpiry     time.Duration
	JWTRefreshExpiry    time.Duration
	LoginMaxAttempts    int
	LoginLockoutTime    time.Duration
	APITokenExpiry      time.Duration
}

// SiteConfig contains site-wide settings.
type SiteConfig struct {
	Name              string
	URL               string
	AllowRegistration bool
	DefaultRole       string
	RequireAuth       bool
}

// UploadConfig contains file upload settings.
type UploadConfig struct {
	Path          string
	MaxSize       int64
	AllowedTypes  []string
	AllowedExtens []string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            getEnvInt("WIKI_PORT", 8080),
			Host:            getEnv("WIKI_HOST", "0.0.0.0"),
			ReadTimeout:     getEnvDuration("WIKI_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    getEnvDuration("WIKI_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: getEnvDuration("WIKI_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Database: DatabaseConfig{
			Path:            getEnv("WIKI_DB_PATH", "./data/wiki.db"),
			MaxOpenConns:    getEnvInt("WIKI_DB_MAX_OPEN", 25),
			MaxIdleConns:    getEnvInt("WIKI_DB_MAX_IDLE", 5),
			ConnMaxLifetime: getEnvDuration("WIKI_DB_CONN_LIFETIME", 5*time.Minute),
		},
		Security: SecurityConfig{
			SecretKey:         getEnv("WIKI_SECRET_KEY", ""),
			SessionName:       getEnv("WIKI_SESSION_NAME", "gowiki_session"),
			SessionMaxAge:     getEnvInt("WIKI_SESSION_MAX_AGE", 86400*7), // 7 days
			CSRFTokenLength:   32,
			BcryptCost:        getEnvInt("WIKI_BCRYPT_COST", 12),
			RateLimitRequests: getEnvInt("WIKI_RATE_LIMIT", 100),
			RateLimitWindow:   getEnvDuration("WIKI_RATE_WINDOW", time.Minute),
			JWTAccessExpiry:   getEnvDuration("WIKI_JWT_ACCESS_EXPIRY", 15*time.Minute),
			JWTRefreshExpiry:  getEnvDuration("WIKI_JWT_REFRESH_EXPIRY", 7*24*time.Hour),
			LoginMaxAttempts:  getEnvInt("WIKI_LOGIN_MAX_ATTEMPTS", 5),
			LoginLockoutTime:  getEnvDuration("WIKI_LOGIN_LOCKOUT", 15*time.Minute),
			APITokenExpiry:    getEnvDuration("WIKI_API_TOKEN_EXPIRY", 90*24*time.Hour), // 90 days
		},
		Site: SiteConfig{
			Name:              getEnv("WIKI_SITE_NAME", "GoWiki"),
			URL:               getEnv("WIKI_SITE_URL", "http://localhost:8080"),
			AllowRegistration: getEnvBool("WIKI_ALLOW_REGISTRATION", false),
			DefaultRole:       getEnv("WIKI_DEFAULT_ROLE", "viewer"),
		},
		Upload: UploadConfig{
			Path:    getEnv("WIKI_UPLOAD_PATH", "./uploads"),
			MaxSize: getEnvInt64("WIKI_MAX_UPLOAD_SIZE", 10*1024*1024), // 10MB
			AllowedTypes: []string{
				"image/jpeg",
				"image/png",
				"image/gif",
				"image/webp",
				"image/svg+xml",
				"application/pdf",
				"text/plain",
				"text/markdown",
			},
			AllowedExtens: []string{
				".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg",
				".pdf", ".txt", ".md",
			},
		},
		Backup: BackupConfig{
			Enabled: getEnvBool("WIKI_BACKUP_ENABLED", true),
			Path:    getEnv("WIKI_BACKUP_PATH", "./backups"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// validate checks that all required configuration is present and valid.
func (c *Config) validate() error {
	var errs []string

	// Generate secret key if not provided (for development only)
	if c.Security.SecretKey == "" {
		key, err := generateRandomKey(32)
		if err != nil {
			errs = append(errs, "failed to generate secret key")
		} else {
			c.Security.SecretKey = key
			fmt.Println("WARNING: No WIKI_SECRET_KEY set, using randomly generated key. Sessions will not persist across restarts.")
		}
	}

	if len(c.Security.SecretKey) < 32 {
		errs = append(errs, "WIKI_SECRET_KEY must be at least 32 characters")
	}

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, "WIKI_PORT must be between 1 and 65535")
	}

	if c.Security.BcryptCost < 10 || c.Security.BcryptCost > 31 {
		errs = append(errs, "WIKI_BCRYPT_COST must be between 10 and 31")
	}

	validRoles := map[string]bool{"admin": true, "editor": true, "viewer": true}
	if !validRoles[c.Site.DefaultRole] {
		errs = append(errs, "WIKI_DEFAULT_ROLE must be one of: admin, editor, viewer")
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

// Address returns the server address string.
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// Helper functions for reading environment variables

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func generateRandomKey(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
