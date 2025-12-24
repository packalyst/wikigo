package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/middleware"
)

// UploadFile handles file uploads.
func (h *Handlers) UploadFile(c echo.Context) error {
	user := middleware.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Not authenticated")
	}

	// Get the file from the request
	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "No file uploaded")
	}

	// Check file size
	if file.Size > h.config.Upload.MaxSize {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("File too large. Maximum size is %d MB", h.config.Upload.MaxSize/(1024*1024)))
	}

	// Open the uploaded file
	src, err := file.Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to read uploaded file")
	}
	defer src.Close()

	// Read first 512 bytes to detect content type
	buffer := make([]byte, 512)
	n, err := src.Read(buffer)
	if err != nil && err != io.EOF {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to read file content")
	}

	// Detect MIME type from content (not from header, which can be spoofed)
	mimeType := http.DetectContentType(buffer[:n])

	// Validate MIME type
	if !h.isAllowedMimeType(mimeType) {
		return echo.NewHTTPError(http.StatusBadRequest, "File type not allowed: "+mimeType)
	}

	// Validate extension
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !h.isAllowedExtension(ext) {
		return echo.NewHTTPError(http.StatusBadRequest, "File extension not allowed: "+ext)
	}

	// Seek back to beginning
	src.Seek(0, io.SeekStart)

	// Generate a safe filename
	safeFilename, err := h.generateSafeFilename(file.Filename)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate filename")
	}

	// Create upload directory if it doesn't exist
	uploadDir := h.config.Upload.Path
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create upload directory")
	}

	// Create the destination file
	destPath := filepath.Join(uploadDir, safeFilename)
	dst, err := os.Create(destPath)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create destination file")
	}
	defer dst.Close()

	// Copy the file
	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(destPath) // Clean up on error
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save file")
	}

	// Return the URL for the uploaded file
	fileURL := "/uploads/" + safeFilename

	// Return JSON response
	return c.JSON(http.StatusOK, map[string]interface{}{
		"success":  true,
		"url":      fileURL,
		"filename": file.Filename,
		"size":     file.Size,
		"mime":     mimeType,
	})
}

// isAllowedMimeType checks if the MIME type is allowed.
func (h *Handlers) isAllowedMimeType(mimeType string) bool {
	// Normalize MIME type (remove parameters like charset)
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	for _, allowed := range h.config.Upload.AllowedTypes {
		if mimeType == allowed {
			return true
		}
	}
	return false
}

// isAllowedExtension checks if the file extension is allowed.
func (h *Handlers) isAllowedExtension(ext string) bool {
	for _, allowed := range h.config.Upload.AllowedExtens {
		if ext == allowed {
			return true
		}
	}
	return false
}

// generateSafeFilename creates a safe, unique filename.
func (h *Handlers) generateSafeFilename(original string) (string, error) {
	// Generate random prefix
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	prefix := hex.EncodeToString(randomBytes)

	// Get extension
	ext := strings.ToLower(filepath.Ext(original))

	// Sanitize base filename
	base := strings.TrimSuffix(original, ext)
	base = sanitizeFilename(base)

	// Truncate if too long
	if len(base) > 50 {
		base = base[:50]
	}

	// Combine: random_sanitized.ext
	return fmt.Sprintf("%s_%s%s", prefix, base, ext), nil
}

// sanitizeFilename removes unsafe characters from a filename.
func sanitizeFilename(name string) string {
	// Replace spaces with underscores
	name = strings.ReplaceAll(name, " ", "_")

	// Only allow alphanumeric, underscore, hyphen, and dot
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result.WriteRune(r)
		}
	}

	sanitized := result.String()
	if sanitized == "" {
		return "file"
	}

	return sanitized
}
