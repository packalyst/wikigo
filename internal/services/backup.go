package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gowiki/internal/config"
	"gowiki/internal/models"
)

// BackupService handles markdown file backups.
type BackupService struct {
	enabled bool
	path    string
}

// NewBackupService creates a new BackupService.
func NewBackupService(cfg *config.Config) (*BackupService, error) {
	if !cfg.Backup.Enabled {
		return &BackupService{enabled: false}, nil
	}

	// Ensure backup directory exists
	if err := os.MkdirAll(cfg.Backup.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	return &BackupService{
		enabled: true,
		path:    cfg.Backup.Path,
	}, nil
}

// SavePageAsMarkdown saves a page's content as a markdown file with YAML frontmatter.
// The pagePath parameter contains parent page slugs for hierarchical folder structure.
func (s *BackupService) SavePageAsMarkdown(page *models.Page, authorName string, pagePath []string) error {
	if !s.enabled {
		return nil
	}

	// Build frontmatter
	var tags []string
	for _, tag := range page.Tags {
		tags = append(tags, tag.Name)
	}

	var frontmatter strings.Builder
	frontmatter.WriteString("---\n")
	frontmatter.WriteString(fmt.Sprintf("title: %q\n", page.Title))
	frontmatter.WriteString(fmt.Sprintf("slug: %q\n", page.Slug))
	frontmatter.WriteString(fmt.Sprintf("author: %q\n", authorName))
	if len(tags) > 0 {
		frontmatter.WriteString(fmt.Sprintf("tags: [%s]\n", strings.Join(quoteTags(tags), ", ")))
	}
	if page.ParentID != nil {
		frontmatter.WriteString(fmt.Sprintf("parent_id: %d\n", *page.ParentID))
	}
	frontmatter.WriteString(fmt.Sprintf("created_at: %s\n", page.CreatedAt.Format(time.RFC3339)))
	frontmatter.WriteString(fmt.Sprintf("updated_at: %s\n", page.UpdatedAt.Format(time.RFC3339)))
	if page.PublishedAt.Valid {
		frontmatter.WriteString(fmt.Sprintf("published_at: %s\n", page.PublishedAt.Time.Format(time.RFC3339)))
	}
	frontmatter.WriteString(fmt.Sprintf("published: %t\n", page.IsPublished))
	frontmatter.WriteString("---\n\n")

	// Combine frontmatter with content
	content := frontmatter.String() + page.Content

	// Build directory path from parent slugs
	dirPath := s.path
	for _, parentSlug := range pagePath {
		dirPath = filepath.Join(dirPath, sanitizeFilename(parentSlug))
	}

	// Create directory structure if needed
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Extract just the last segment of the slug for the filename
	slugParts := strings.Split(page.Slug, "/")
	finalName := slugParts[len(slugParts)-1]
	filename := sanitizeFilename(finalName) + ".md"
	filePath := filepath.Join(dirPath, filename)

	// Write file
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	return nil
}

// DeleteBackup removes the markdown backup file for a page.
// The pagePath parameter contains parent page slugs for hierarchical folder structure.
func (s *BackupService) DeleteBackup(slug string, pagePath []string) error {
	if !s.enabled {
		return nil
	}

	// Build directory path from parent slugs
	dirPath := s.path
	for _, parentSlug := range pagePath {
		dirPath = filepath.Join(dirPath, sanitizeFilename(parentSlug))
	}

	// Extract just the last segment of the slug for the filename
	slugParts := strings.Split(slug, "/")
	finalName := slugParts[len(slugParts)-1]
	filename := sanitizeFilename(finalName) + ".md"
	filePath := filepath.Join(dirPath, filename)

	// Remove file if it exists, ignore if it doesn't
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete backup file: %w", err)
	}

	// Try to remove empty parent directories
	s.cleanEmptyDirs(dirPath)

	return nil
}

// cleanEmptyDirs removes empty directories up to the backup root.
func (s *BackupService) cleanEmptyDirs(dirPath string) {
	for dirPath != s.path {
		entries, err := os.ReadDir(dirPath)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(dirPath)
		dirPath = filepath.Dir(dirPath)
	}
}

// quoteTags adds quotes around each tag for YAML array format.
func quoteTags(tags []string) []string {
	quoted := make([]string, len(tags))
	for i, tag := range tags {
		quoted[i] = fmt.Sprintf("%q", tag)
	}
	return quoted
}

// sanitizeFilename makes a slug safe for use as a filename.
func sanitizeFilename(slug string) string {
	// Replace any path separators with dashes
	slug = strings.ReplaceAll(slug, "/", "-")
	slug = strings.ReplaceAll(slug, "\\", "-")
	// Remove any other potentially problematic characters
	slug = strings.Map(func(r rune) rune {
		if r == '<' || r == '>' || r == ':' || r == '"' || r == '|' || r == '?' || r == '*' {
			return '-'
		}
		return r
	}, slug)
	return slug
}
