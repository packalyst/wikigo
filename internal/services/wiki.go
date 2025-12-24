package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gowiki/internal/database"
	"gowiki/internal/models"
)

// Wiki errors.
var (
	ErrPageNotFound     = errors.New("page not found")
	ErrPageExists       = errors.New("page with this slug already exists")
	ErrInvalidSlug      = errors.New("invalid page slug")
	ErrInvalidTitle     = errors.New("page title is required")
	ErrRevisionNotFound = errors.New("revision not found")
)

// SlugChange represents a slug that was changed during an update.
type SlugChange struct {
	OldSlug string
	NewSlug string
}

// UpdateResult contains the result of an update operation including any cascaded changes.
type UpdateResult struct {
	Page        *models.Page
	SlugChanges []SlugChange // Includes all cascaded child slug changes
}

// WikiService handles wiki page operations.
type WikiService struct {
	db       *database.DB
	markdown *MarkdownService
}

// NewWikiService creates a new wiki service.
func NewWikiService(db *database.DB, markdown *MarkdownService) *WikiService {
	return &WikiService{
		db:       db,
		markdown: markdown,
	}
}

// GetDB returns the database instance.
func (s *WikiService) GetDB() *database.DB {
	return s.db
}

// CreatePage creates a new wiki page.
// If the slug contains slashes (e.g., "linux/ubuntu/networking"), parent pages are auto-created.
func (s *WikiService) CreatePage(ctx context.Context, authorID int64, input models.PageCreate) (*models.Page, error) {
	// Validate and normalize slug
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		// Generate from title
		slug = Slugify(input.Title)
	} else {
		slug = Slugify(slug)
	}

	if slug == "" {
		return nil, ErrInvalidSlug
	}

	// Validate title
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, ErrInvalidTitle
	}

	// Check if slug already exists
	existing, err := s.db.GetPageBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing page: %w", err)
	}
	if existing != nil {
		return nil, ErrPageExists
	}

	// Auto-create parent pages if slug contains path separators
	var parentID *int64
	if strings.Contains(slug, "/") {
		parentID, err = s.ensureParentPages(ctx, authorID, slug)
		if err != nil {
			return nil, fmt.Errorf("failed to create parent pages: %w", err)
		}
	}

	// Render markdown to HTML
	contentHTML, err := s.markdown.Render(input.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to render markdown: %w", err)
	}

	page := &models.Page{
		Slug:        slug,
		Title:       title,
		Content:     input.Content,
		ContentHTML: contentHTML,
		AuthorID:    authorID,
		ParentID:    parentID,
		IsPublished: true,
	}

	if err := s.db.CreatePage(ctx, page); err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}

	// Save initial revision
	revision := &models.Revision{
		PageID:   page.ID,
		Content:  input.Content,
		AuthorID: authorID,
		Comment:  "Initial version",
	}
	if err := s.db.CreateRevision(ctx, revision); err != nil {
		// Log but don't fail
		fmt.Printf("Warning: failed to create initial revision: %v\n", err)
	}

	// Set tags if provided
	if len(input.Tags) > 0 {
		if err := s.db.SetPageTags(ctx, page.ID, input.Tags); err != nil {
			fmt.Printf("Warning: failed to set tags: %v\n", err)
		}
		// Load tags into page object
		tags, _ := s.db.GetPageTags(ctx, page.ID)
		page.Tags = tags
	}

	return page, nil
}

// GetPage retrieves a page by slug.
func (s *WikiService) GetPage(ctx context.Context, slug string) (*models.Page, error) {
	page, err := s.db.GetPageBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to get page: %w", err)
	}
	if page == nil {
		return nil, ErrPageNotFound
	}
	return page, nil
}

// GetPageByID retrieves a page by ID.
func (s *WikiService) GetPageByID(ctx context.Context, id int64) (*models.Page, error) {
	page, err := s.db.GetPageByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get page: %w", err)
	}
	if page == nil {
		return nil, ErrPageNotFound
	}
	return page, nil
}

// UpdatePage updates an existing page.
// Returns UpdateResult containing the page and any cascaded slug changes.
func (s *WikiService) UpdatePage(ctx context.Context, pageID, authorID int64, input models.PageUpdate, comment string) (*UpdateResult, error) {
	page, err := s.db.GetPageByID(ctx, pageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get page: %w", err)
	}
	if page == nil {
		return nil, ErrPageNotFound
	}

	var slugChanges []SlugChange

	// Handle slug change
	if input.Slug != nil {
		newSlug := Slugify(*input.Slug)
		if newSlug != "" && newSlug != page.Slug {
			oldSlug := page.Slug

			// Check for collision (but allow updating self)
			existing, err := s.db.GetPageBySlug(ctx, newSlug)
			if err != nil {
				return nil, fmt.Errorf("failed to check slug: %w", err)
			}
			if existing != nil && existing.ID != pageID {
				return nil, ErrPageExists
			}

			// Resolve new parent from slug hierarchy
			var newParentID *int64
			if strings.Contains(newSlug, "/") {
				newParentID, err = s.ensureParentPages(ctx, authorID, newSlug)
				if err != nil {
					return nil, fmt.Errorf("failed to create parent pages: %w", err)
				}
			}
			// If no "/" in slug, newParentID stays nil (becomes root level)

			page.Slug = newSlug
			page.ParentID = newParentID

			// Cascade update: update all descendant slugs
			descendants, err := s.db.GetAllDescendants(ctx, pageID)
			if err != nil {
				return nil, fmt.Errorf("failed to get descendants: %w", err)
			}

			for _, desc := range descendants {
				// Replace old parent slug prefix with new one
				// e.g., if oldSlug="linux" and newSlug="commands/linux"
				// then child "linux/docker" becomes "commands/linux/docker"
				if strings.HasPrefix(desc.Slug, oldSlug+"/") {
					newChildSlug := newSlug + strings.TrimPrefix(desc.Slug, oldSlug)
					if err := s.db.UpdatePageSlug(ctx, desc.ID, newChildSlug); err != nil {
						return nil, fmt.Errorf("failed to update descendant slug: %w", err)
					}
					// Track the change for backup updates
					slugChanges = append(slugChanges, SlugChange{
						OldSlug: desc.Slug,
						NewSlug: newChildSlug,
					})
					fmt.Printf("Cascade updated slug: %s -> %s\n", desc.Slug, newChildSlug)
				}
			}
		}
	}

	// Save revision before update
	if input.Content != nil && *input.Content != page.Content {
		revision := &models.Revision{
			PageID:   page.ID,
			Content:  page.Content, // Save the old content
			AuthorID: authorID,
			Comment:  comment,
		}
		if err := s.db.CreateRevision(ctx, revision); err != nil {
			fmt.Printf("Warning: failed to create revision: %v\n", err)
		}
	}

	// Update fields
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return nil, ErrInvalidTitle
		}
		page.Title = title
	}

	if input.Content != nil {
		page.Content = *input.Content
		contentHTML, err := s.markdown.Render(*input.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to render markdown: %w", err)
		}
		page.ContentHTML = contentHTML
	}

	if input.IsPublished != nil {
		page.IsPublished = *input.IsPublished
		if *input.IsPublished && !page.PublishedAt.Valid {
			page.PublishedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
		}
	}

	page.UpdatedAt = time.Now().UTC()

	if err := s.db.UpdatePage(ctx, page); err != nil {
		return nil, fmt.Errorf("failed to update page: %w", err)
	}

	// Update tags if provided
	if input.Tags != nil {
		if err := s.db.SetPageTags(ctx, page.ID, input.Tags); err != nil {
			fmt.Printf("Warning: failed to update tags: %v\n", err)
		}
	}

	// Load tags into page object for backup
	tags, _ := s.db.GetPageTags(ctx, page.ID)
	page.Tags = tags

	return &UpdateResult{
		Page:        page,
		SlugChanges: slugChanges,
	}, nil
}

// DeletePage removes a page.
func (s *WikiService) DeletePage(ctx context.Context, pageID int64) error {
	page, err := s.db.GetPageByID(ctx, pageID)
	if err != nil {
		return fmt.Errorf("failed to get page: %w", err)
	}
	if page == nil {
		return ErrPageNotFound
	}

	return s.db.DeletePage(ctx, pageID)
}

// ListPages retrieves pages with filtering.
func (s *WikiService) ListPages(ctx context.Context, filter models.PageFilter) ([]models.PageSummary, error) {
	return s.db.ListPages(ctx, filter)
}

// GetRecentPages retrieves the most recently updated pages.
func (s *WikiService) GetRecentPages(ctx context.Context, limit int) ([]models.PageSummary, error) {
	filter := models.NewPageFilter()
	filter.Limit = limit
	published := true
	filter.IsPublished = &published

	return s.db.ListPages(ctx, filter)
}

// GetPageRevisions retrieves revision history for a page.
func (s *WikiService) GetPageRevisions(ctx context.Context, pageID int64, limit, offset int) ([]models.RevisionSummary, error) {
	return s.db.ListRevisions(ctx, pageID, limit, offset)
}

// GetRevision retrieves a specific revision.
func (s *WikiService) GetRevision(ctx context.Context, revisionID int64) (*models.Revision, error) {
	rev, err := s.db.GetRevision(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get revision: %w", err)
	}
	if rev == nil {
		return nil, ErrRevisionNotFound
	}
	return rev, nil
}

// RevertToRevision reverts a page to a previous revision.
func (s *WikiService) RevertToRevision(ctx context.Context, revisionID, authorID int64) (*models.Page, error) {
	rev, err := s.GetRevision(ctx, revisionID)
	if err != nil {
		return nil, err
	}

	content := rev.Content
	comment := fmt.Sprintf("Reverted to revision %d", revisionID)

	result, err := s.UpdatePage(ctx, rev.PageID, authorID, models.PageUpdate{
		Content: &content,
	}, comment)
	if err != nil {
		return nil, err
	}
	return result.Page, nil
}

// Search performs full-text search on pages.
func (s *WikiService) Search(ctx context.Context, query string, limit int) ([]models.SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	return s.db.SearchPages(ctx, query, limit)
}

// GetAllTags retrieves all tags with page counts.
func (s *WikiService) GetAllTags(ctx context.Context) ([]models.Tag, error) {
	return s.db.ListTags(ctx)
}

// GetPagesByTag retrieves pages with a specific tag.
func (s *WikiService) GetPagesByTag(ctx context.Context, tag string, limit, offset int) ([]models.PageSummary, error) {
	filter := models.NewPageFilter()
	filter.Tag = &tag
	filter.Limit = limit
	filter.Offset = offset
	published := true
	filter.IsPublished = &published

	return s.db.ListPages(ctx, filter)
}

// RenderMarkdown renders markdown content to HTML.
func (s *WikiService) RenderMarkdown(content string) (string, error) {
	return s.markdown.Render(content)
}

// GenerateTOC generates a table of contents from markdown.
func (s *WikiService) GenerateTOC(content string) []TOCEntry {
	return s.markdown.GenerateTOC(content)
}

// GetBacklinks finds pages that link to a given page.
func (s *WikiService) GetBacklinks(ctx context.Context, slug string) ([]models.PageSummary, error) {
	// Search for pages containing the wiki link
	searchPattern := "[[" + slug

	filter := models.NewPageFilter()
	filter.Search = &searchPattern
	published := true
	filter.IsPublished = &published

	return s.db.ListPages(ctx, filter)
}

// PageExists checks if a page with the given slug exists.
func (s *WikiService) PageExists(ctx context.Context, slug string) (bool, error) {
	page, err := s.db.GetPageBySlug(ctx, slug)
	if err != nil {
		return false, err
	}
	return page != nil, nil
}

// GetStats returns wiki statistics.
func (s *WikiService) GetStats(ctx context.Context) (*WikiStats, error) {
	pageCount, err := s.db.CountPages(ctx)
	if err != nil {
		return nil, err
	}

	userCount, err := s.db.CountUsers(ctx)
	if err != nil {
		return nil, err
	}

	tags, err := s.db.ListTags(ctx)
	if err != nil {
		return nil, err
	}

	return &WikiStats{
		PageCount: pageCount,
		UserCount: userCount,
		TagCount:  len(tags),
	}, nil
}

// WikiStats contains wiki statistics.
type WikiStats struct {
	PageCount int
	UserCount int
	TagCount  int
}

// ImportDocumentation imports README.md and API.md as wiki pages on first run.
func (s *WikiService) ImportDocumentation(ctx context.Context, adminID int64) error {
	// Import README.md
	if err := s.importDocFile(ctx, adminID, "README.md", "readme", "About GoWiki", []string{"documentation"}); err != nil {
		fmt.Printf("Warning: failed to import README.md: %v\n", err)
	}

	// Import API.md
	if err := s.importDocFile(ctx, adminID, "API.md", "api-documentation", "API Documentation", []string{"documentation", "api"}); err != nil {
		fmt.Printf("Warning: failed to import API.md: %v\n", err)
	}

	return nil
}

// importDocFile imports a markdown file as a wiki page if it doesn't exist.
func (s *WikiService) importDocFile(ctx context.Context, authorID int64, filepath, slug, title string, tags []string) error {
	// Check if page already exists
	exists, err := s.PageExists(ctx, slug)
	if err != nil {
		return err
	}
	if exists {
		return nil // Page already exists, skip
	}

	// Read the file
	content, err := readFileIfExists(filepath)
	if err != nil {
		return err
	}
	if content == "" {
		return nil // File doesn't exist, skip
	}

	// Create the page
	_, err = s.CreatePage(ctx, authorID, models.PageCreate{
		Title:   title,
		Slug:    slug,
		Content: content,
		Tags:    tags,
	})

	if err != nil && !errors.Is(err, ErrPageExists) {
		return err
	}

	fmt.Printf("Imported documentation: %s -> /wiki/%s\n", filepath, slug)
	return nil
}

// readFileIfExists reads a file if it exists, returns empty string if not.
func readFileIfExists(filepath string) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(content), nil
}

// ensureParentPages creates all parent pages in a hierarchical path.
// For slug "linux/ubuntu/networking", it creates "linux" and "linux/ubuntu" if they don't exist.
// Returns the ID of the immediate parent page.
func (s *WikiService) ensureParentPages(ctx context.Context, authorID int64, slug string) (*int64, error) {
	parts := strings.Split(slug, "/")
	if len(parts) <= 1 {
		return nil, nil // No parents needed
	}

	var parentID *int64

	// Process all parts except the last one (which is the page being created)
	for i := 0; i < len(parts)-1; i++ {
		parentSlug := strings.Join(parts[:i+1], "/")

		// Check if parent exists
		existing, err := s.db.GetPageBySlug(ctx, parentSlug)
		if err != nil {
			return nil, err
		}

		if existing != nil {
			// Parent exists, use its ID
			parentID = &existing.ID
		} else {
			// Create parent page with title derived from slug segment
			title := strings.Title(strings.ReplaceAll(parts[i], "-", " "))

			contentHTML, _ := s.markdown.Render("")

			parent := &models.Page{
				Slug:        parentSlug,
				Title:       title,
				Content:     "",
				ContentHTML: contentHTML,
				AuthorID:    authorID,
				ParentID:    parentID,
				IsPublished: true,
			}

			if err := s.db.CreatePage(ctx, parent); err != nil {
				return nil, fmt.Errorf("failed to create parent page %s: %w", parentSlug, err)
			}

			parentID = &parent.ID
			fmt.Printf("Auto-created parent page: %s\n", parentSlug)
		}
	}

	return parentID, nil
}
