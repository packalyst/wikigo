package models

import (
	"database/sql"
	"time"
)

// Page represents a wiki page.
type Page struct {
	ID          int64        `json:"id"`
	Slug        string       `json:"slug"`
	Title       string       `json:"title"`
	Content     string       `json:"content"`      // Raw markdown
	ContentHTML string       `json:"content_html"` // Rendered HTML
	AuthorID    int64        `json:"author_id"`
	Author      *User        `json:"author,omitempty"`
	ParentID    *int64       `json:"parent_id,omitempty"`
	Parent      *Page        `json:"parent,omitempty"`
	Children    []Page       `json:"children,omitempty"`
	IsPublished bool         `json:"is_published"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	PublishedAt sql.NullTime `json:"published_at,omitempty"`
	Tags        []Tag        `json:"tags,omitempty"`
}

// PageCreate contains data for creating a new page.
type PageCreate struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	ParentID *int64   `json:"parent_id,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// PageUpdate contains data for updating a page.
type PageUpdate struct {
	Slug        *string  `json:"slug,omitempty"`
	Title       *string  `json:"title,omitempty"`
	Content     *string  `json:"content,omitempty"`
	ParentID    *int64   `json:"parent_id,omitempty"`
	IsPublished *bool    `json:"is_published,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// PageSummary contains minimal page info for listings.
type PageSummary struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Author    string    `json:"author"`
}

// Revision represents a page version in history.
type Revision struct {
	ID        int64     `json:"id"`
	PageID    int64     `json:"page_id"`
	Content   string    `json:"content"`
	AuthorID  int64     `json:"author_id"`
	Author    *User     `json:"author,omitempty"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// RevisionSummary contains minimal revision info for history lists.
type RevisionSummary struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// Tag represents a page tag.
type Tag struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	PageCount int    `json:"page_count,omitempty"`
}

// Attachment represents a file attached to a page.
type Attachment struct {
	ID         int64     `json:"id"`
	PageID     int64     `json:"page_id"`
	Filename   string    `json:"filename"`
	Filepath   string    `json:"-"` // Internal path, not exposed
	MimeType   string    `json:"mime_type"`
	SizeBytes  int64     `json:"size_bytes"`
	UploaderID int64     `json:"uploader_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// SearchResult represents a full-text search hit.
type SearchResult struct {
	PageID    int64   `json:"page_id"`
	Slug      string  `json:"slug"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet"`
	Rank      float64 `json:"rank"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PageFilter contains options for filtering page queries.
type PageFilter struct {
	AuthorID    *int64
	IsPublished *bool
	Tag         *string
	Search      *string
	Limit       int
	Offset      int
	OrderBy     string
	OrderDir    string
}

// NewPageFilter creates a filter with sensible defaults.
func NewPageFilter() PageFilter {
	return PageFilter{
		Limit:    20,
		Offset:   0,
		OrderBy:  "updated_at",
		OrderDir: "DESC",
	}
}
