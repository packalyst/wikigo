package database

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gowiki/internal/models"
)

var (
	mdHeaderRegex = regexp.MustCompile(`^#{1,6}\s+`)
	mdLinkRegex   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdBoldRegex   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdItalicRegex = regexp.MustCompile(`\*([^*]+)\*`)
	mdCodeRegex   = regexp.MustCompile("`[^`]+`")
)

// cleanExcerpt removes markdown formatting and cleans up an excerpt.
func cleanExcerpt(raw string) string {
	// Split into lines
	lines := strings.Split(raw, "\n")
	var cleanLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and headers
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Remove markdown formatting
		line = mdLinkRegex.ReplaceAllString(line, "$1")
		line = mdBoldRegex.ReplaceAllString(line, "$1")
		line = mdItalicRegex.ReplaceAllString(line, "$1")
		line = mdCodeRegex.ReplaceAllString(line, "")

		cleanLines = append(cleanLines, line)
	}

	excerpt := strings.Join(cleanLines, " ")
	// Truncate to ~150 chars at word boundary
	if len(excerpt) > 150 {
		excerpt = excerpt[:150]
		if idx := strings.LastIndex(excerpt, " "); idx > 100 {
			excerpt = excerpt[:idx]
		}
		excerpt += "..."
	}
	return excerpt
}

// User queries

// CreateUser inserts a new user into the database.
func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	result, err := db.ExecContext(ctx, `
		INSERT INTO users (username, email, password_hash, role, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, user.Username, user.Email, user.PasswordHash, user.Role, user.IsActive, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get user ID: %w", err)
	}

	user.ID = id
	return nil
}

// GetUserByID retrieves a user by ID.
func (db *DB) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	user := &models.User{}
	err := db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, created_at, updated_at, last_login_at
		FROM users WHERE id = ?
	`, id).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// GetUserByUsername retrieves a user by username.
func (db *DB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	user := &models.User{}
	err := db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, created_at, updated_at, last_login_at
		FROM users WHERE username = ? COLLATE NOCASE
	`, username).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// GetUserByEmail retrieves a user by email.
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user := &models.User{}
	err := db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, created_at, updated_at, last_login_at
		FROM users WHERE email = ? COLLATE NOCASE
	`, email).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// UpdateUserLastLogin updates the user's last login timestamp.
func (db *DB) UpdateUserLastLogin(ctx context.Context, userID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE users SET last_login_at = ? WHERE id = ?
	`, time.Now().UTC(), userID)
	return err
}

// ListUsers retrieves all users.
func (db *DB) ListUsers(ctx context.Context, limit, offset int) ([]models.User, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, created_at, updated_at, last_login_at
		FROM users
		ORDER BY username ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(
			&u.ID, &u.Username, &u.Email, &u.PasswordHash,
			&u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, u)
	}

	return users, rows.Err()
}

// UpdateUser updates user fields.
func (db *DB) UpdateUser(ctx context.Context, id int64, update *models.UserUpdate) error {
	var setClauses []string
	var args []interface{}

	if update.Email != nil {
		setClauses = append(setClauses, "email = ?")
		args = append(args, *update.Email)
	}
	if update.Password != nil {
		setClauses = append(setClauses, "password_hash = ?")
		args = append(args, *update.Password)
	}
	if update.Role != nil {
		setClauses = append(setClauses, "role = ?")
		args = append(args, *update.Role)
	}
	if update.IsActive != nil {
		setClauses = append(setClauses, "is_active = ?")
		args = append(args, *update.IsActive)
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, time.Now().UTC())
	args = append(args, id)

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

// DeleteUser removes a user by ID.
func (db *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	return err
}

// Page queries

// CreatePage inserts a new page.
func (db *DB) CreatePage(ctx context.Context, page *models.Page) error {
	now := time.Now().UTC()
	page.CreatedAt = now
	page.UpdatedAt = now

	if page.IsPublished {
		page.PublishedAt = sql.NullTime{Time: now, Valid: true}
	}

	result, err := db.ExecContext(ctx, `
		INSERT INTO pages (slug, title, content, content_html, author_id, parent_id, is_published, created_at, updated_at, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, page.Slug, page.Title, page.Content, page.ContentHTML, page.AuthorID, page.ParentID,
		page.IsPublished, page.CreatedAt, page.UpdatedAt, page.PublishedAt)
	if err != nil {
		return fmt.Errorf("failed to create page: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get page ID: %w", err)
	}

	page.ID = id
	return nil
}

// GetPageByID retrieves a page by ID.
func (db *DB) GetPageByID(ctx context.Context, id int64) (*models.Page, error) {
	page := &models.Page{}
	err := db.QueryRowContext(ctx, `
		SELECT p.id, p.slug, p.title, p.content, p.content_html, p.author_id, p.parent_id,
			   p.is_published, p.created_at, p.updated_at, p.published_at,
			   u.username
		FROM pages p
		JOIN users u ON p.author_id = u.id
		WHERE p.id = ?
	`, id).Scan(
		&page.ID, &page.Slug, &page.Title, &page.Content, &page.ContentHTML,
		&page.AuthorID, &page.ParentID, &page.IsPublished, &page.CreatedAt, &page.UpdatedAt,
		&page.PublishedAt, new(string),
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get page: %w", err)
	}

	// Load tags
	tags, err := db.GetPageTags(ctx, page.ID)
	if err != nil {
		return nil, err
	}
	page.Tags = tags

	return page, nil
}

// GetPageBySlug retrieves a page by slug.
func (db *DB) GetPageBySlug(ctx context.Context, slug string) (*models.Page, error) {
	page := &models.Page{}
	var authorUsername string

	err := db.QueryRowContext(ctx, `
		SELECT p.id, p.slug, p.title, p.content, p.content_html, p.author_id, p.parent_id,
			   p.is_published, p.created_at, p.updated_at, p.published_at,
			   u.username
		FROM pages p
		JOIN users u ON p.author_id = u.id
		WHERE p.slug = ? COLLATE NOCASE
	`, slug).Scan(
		&page.ID, &page.Slug, &page.Title, &page.Content, &page.ContentHTML,
		&page.AuthorID, &page.ParentID, &page.IsPublished, &page.CreatedAt, &page.UpdatedAt,
		&page.PublishedAt, &authorUsername,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get page: %w", err)
	}

	page.Author = &models.User{ID: page.AuthorID, Username: authorUsername}

	// Load tags
	tags, err := db.GetPageTags(ctx, page.ID)
	if err != nil {
		return nil, err
	}
	page.Tags = tags

	return page, nil
}

// UpdatePage updates a page.
func (db *DB) UpdatePage(ctx context.Context, page *models.Page) error {
	page.UpdatedAt = time.Now().UTC()

	_, err := db.ExecContext(ctx, `
		UPDATE pages
		SET slug = ?, title = ?, content = ?, content_html = ?, parent_id = ?, is_published = ?, updated_at = ?, published_at = ?
		WHERE id = ?
	`, page.Slug, page.Title, page.Content, page.ContentHTML, page.ParentID, page.IsPublished, page.UpdatedAt, page.PublishedAt, page.ID)

	return err
}

// DeletePage removes a page by ID.
func (db *DB) DeletePage(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM pages WHERE id = ?", id)
	return err
}

// DeletePages removes multiple pages by ID within a transaction.
// Pages should be ordered children-first, parents-last for proper cascade.
func (db *DB) DeletePages(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	return db.Transaction(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "DELETE FROM pages WHERE id = ?", id); err != nil {
				return fmt.Errorf("failed to delete page %d: %w", id, err)
			}
		}
		return nil
	})
}

// ListPages retrieves pages with optional filtering.
func (db *DB) ListPages(ctx context.Context, filter models.PageFilter) ([]models.PageSummary, error) {
	var whereClauses []string
	var args []interface{}

	if filter.IsPublished != nil {
		whereClauses = append(whereClauses, "p.is_published = ?")
		args = append(args, *filter.IsPublished)
	}

	if filter.AuthorID != nil {
		whereClauses = append(whereClauses, "p.author_id = ?")
		args = append(args, *filter.AuthorID)
	}

	if filter.Tag != nil {
		whereClauses = append(whereClauses, `
			EXISTS (
				SELECT 1 FROM page_tags pt
				JOIN tags t ON pt.tag_id = t.id
				WHERE pt.page_id = p.id AND t.name = ? COLLATE NOCASE
			)
		`)
		args = append(args, *filter.Tag)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Validate order by to prevent SQL injection
	validOrderBy := map[string]bool{"updated_at": true, "created_at": true, "title": true}
	orderBy := "updated_at"
	if validOrderBy[filter.OrderBy] {
		orderBy = filter.OrderBy
	}

	orderDir := "DESC"
	if filter.OrderDir == "ASC" {
		orderDir = "ASC"
	}

	query := fmt.Sprintf(`
		SELECT p.id, p.slug, p.title, SUBSTR(p.content, 1, 200), p.parent_id, p.updated_at, u.username
		FROM pages p
		JOIN users u ON p.author_id = u.id
		%s
		ORDER BY p.%s %s
		LIMIT ? OFFSET ?
	`, whereSQL, orderBy, orderDir)

	args = append(args, filter.Limit, filter.Offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list pages: %w", err)
	}
	defer rows.Close()

	var pages []models.PageSummary
	for rows.Next() {
		var p models.PageSummary
		var rawExcerpt string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &rawExcerpt, &p.ParentID, &p.UpdatedAt, &p.Author); err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		// Clean up the excerpt - remove markdown headers and trim
		p.Excerpt = cleanExcerpt(rawExcerpt)
		pages = append(pages, p)
	}

	return pages, rows.Err()
}

// GetAllDescendants retrieves all descendant pages of a given page using recursive CTE.
// Returns pages with their IDs and slugs for bulk updates.
func (db *DB) GetAllDescendants(ctx context.Context, parentID int64) ([]struct {
	ID   int64
	Slug string
}, error) {
	rows, err := db.QueryContext(ctx, `
		WITH RECURSIVE descendants AS (
			SELECT id, slug
			FROM pages
			WHERE parent_id = ?
			UNION ALL
			SELECT p.id, p.slug
			FROM pages p
			JOIN descendants d ON p.parent_id = d.id
		)
		SELECT id, slug FROM descendants
	`, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get descendants: %w", err)
	}
	defer rows.Close()

	var result []struct {
		ID   int64
		Slug string
	}
	for rows.Next() {
		var item struct {
			ID   int64
			Slug string
		}
		if err := rows.Scan(&item.ID, &item.Slug); err != nil {
			return nil, fmt.Errorf("failed to scan descendant: %w", err)
		}
		result = append(result, item)
	}

	return result, rows.Err()
}

// UpdatePageSlug updates just the slug of a page (used for cascade updates).
func (db *DB) UpdatePageSlug(ctx context.Context, pageID int64, newSlug string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE pages SET slug = ?, updated_at = ? WHERE id = ?
	`, newSlug, time.Now().UTC(), pageID)
	return err
}

// GetPageChildren retrieves child pages of a given page.
func (db *DB) GetPageChildren(ctx context.Context, parentID int64) ([]models.PageSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.title, SUBSTR(p.content, 1, 200), p.parent_id, p.updated_at, u.username
		FROM pages p
		JOIN users u ON p.author_id = u.id
		WHERE p.parent_id = ?
		ORDER BY p.title ASC
	`, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get child pages: %w", err)
	}
	defer rows.Close()

	var pages []models.PageSummary
	for rows.Next() {
		var p models.PageSummary
		var rawExcerpt string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &rawExcerpt, &p.ParentID, &p.UpdatedAt, &p.Author); err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		p.Excerpt = cleanExcerpt(rawExcerpt)
		pages = append(pages, p)
	}

	return pages, rows.Err()
}

// GetPagePath retrieves the full path (breadcrumb) for a page using a recursive CTE.
// This replaces the N+1 query loop with a single query.
func (db *DB) GetPagePath(ctx context.Context, pageID int64) ([]models.PageSummary, error) {
	rows, err := db.QueryContext(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT id, slug, title, parent_id, 0 as depth
			FROM pages
			WHERE id = ?
			UNION ALL
			SELECT p.id, p.slug, p.title, p.parent_id, a.depth + 1
			FROM pages p
			JOIN ancestors a ON p.id = a.parent_id
		)
		SELECT id, slug, title FROM ancestors ORDER BY depth DESC
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get page path: %w", err)
	}
	defer rows.Close()

	var path []models.PageSummary
	for rows.Next() {
		var p models.PageSummary
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title); err != nil {
			return nil, fmt.Errorf("failed to scan page path: %w", err)
		}
		path = append(path, p)
	}

	return path, rows.Err()
}

// GetRootPages retrieves pages without a parent.
func (db *DB) GetRootPages(ctx context.Context) ([]models.PageSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.title, SUBSTR(p.content, 1, 200), p.parent_id, p.updated_at, u.username
		FROM pages p
		JOIN users u ON p.author_id = u.id
		WHERE p.parent_id IS NULL
		ORDER BY p.title ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get root pages: %w", err)
	}
	defer rows.Close()

	var pages []models.PageSummary
	for rows.Next() {
		var p models.PageSummary
		var rawExcerpt string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &rawExcerpt, &p.ParentID, &p.UpdatedAt, &p.Author); err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		p.Excerpt = cleanExcerpt(rawExcerpt)
		pages = append(pages, p)
	}

	return pages, rows.Err()
}

// GetAllPageSummaries returns a summary of all pages for selection purposes.
func (db *DB) GetAllPageSummaries(ctx context.Context) ([]models.PageSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.title, '', p.parent_id, p.updated_at, u.username
		FROM pages p
		JOIN users u ON p.author_id = u.id
		ORDER BY p.title ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get all pages: %w", err)
	}
	defer rows.Close()

	var pages []models.PageSummary
	for rows.Next() {
		var p models.PageSummary
		var excerpt string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &excerpt, &p.ParentID, &p.UpdatedAt, &p.Author); err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}
		pages = append(pages, p)
	}

	return pages, rows.Err()
}

// Revision queries

// CreateRevision saves a page revision.
func (db *DB) CreateRevision(ctx context.Context, rev *models.Revision) error {
	rev.CreatedAt = time.Now().UTC()

	result, err := db.ExecContext(ctx, `
		INSERT INTO revisions (page_id, content, author_id, comment, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, rev.PageID, rev.Content, rev.AuthorID, rev.Comment, rev.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create revision: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get revision ID: %w", err)
	}

	rev.ID = id
	return nil
}

// GetRevision retrieves a revision by ID.
func (db *DB) GetRevision(ctx context.Context, id int64) (*models.Revision, error) {
	rev := &models.Revision{}
	var authorUsername string

	err := db.QueryRowContext(ctx, `
		SELECT r.id, r.page_id, r.content, r.author_id, r.comment, r.created_at, u.username
		FROM revisions r
		JOIN users u ON r.author_id = u.id
		WHERE r.id = ?
	`, id).Scan(&rev.ID, &rev.PageID, &rev.Content, &rev.AuthorID, &rev.Comment, &rev.CreatedAt, &authorUsername)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get revision: %w", err)
	}

	rev.Author = &models.User{ID: rev.AuthorID, Username: authorUsername}
	return rev, nil
}

// ListRevisions retrieves revisions for a page.
func (db *DB) ListRevisions(ctx context.Context, pageID int64, limit, offset int) ([]models.RevisionSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT r.id, u.username, r.comment, r.created_at
		FROM revisions r
		JOIN users u ON r.author_id = u.id
		WHERE r.page_id = ?
		ORDER BY r.created_at DESC
		LIMIT ? OFFSET ?
	`, pageID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list revisions: %w", err)
	}
	defer rows.Close()

	var revisions []models.RevisionSummary
	for rows.Next() {
		var r models.RevisionSummary
		if err := rows.Scan(&r.ID, &r.Author, &r.Comment, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan revision: %w", err)
		}
		revisions = append(revisions, r)
	}

	return revisions, rows.Err()
}

// Tag queries

// GetOrCreateTag gets an existing tag or creates a new one.
func (db *DB) GetOrCreateTag(ctx context.Context, name string) (*models.Tag, error) {
	tag := &models.Tag{}

	err := db.QueryRowContext(ctx, "SELECT id, name FROM tags WHERE name = ? COLLATE NOCASE", name).Scan(&tag.ID, &tag.Name)
	if err == nil {
		return tag, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// Create new tag
	result, err := db.ExecContext(ctx, "INSERT INTO tags (name) VALUES (?)", name)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	tag.ID = id
	tag.Name = name

	return tag, nil
}

// SetPageTags replaces all tags for a page within a transaction.
func (db *DB) SetPageTags(ctx context.Context, pageID int64, tagNames []string) error {
	return db.Transaction(ctx, func(tx *sql.Tx) error {
		// Remove existing tags
		if _, err := tx.ExecContext(ctx, "DELETE FROM page_tags WHERE page_id = ?", pageID); err != nil {
			return err
		}

		// Add new tags
		for _, name := range tagNames {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}

			// Get or create tag within transaction
			tag, err := db.getOrCreateTagTx(ctx, tx, name)
			if err != nil {
				return err
			}

			_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO page_tags (page_id, tag_id) VALUES (?, ?)", pageID, tag.ID)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// getOrCreateTagTx gets or creates a tag within a transaction.
func (db *DB) getOrCreateTagTx(ctx context.Context, tx *sql.Tx, name string) (*models.Tag, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	// Try to find existing tag
	var tag models.Tag
	err := tx.QueryRowContext(ctx, "SELECT id, name FROM tags WHERE name = ? COLLATE NOCASE", name).Scan(&tag.ID, &tag.Name)
	if err == nil {
		return &tag, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// Create new tag
	result, err := tx.ExecContext(ctx, "INSERT INTO tags (name) VALUES (?)", name)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	tag.ID = id
	tag.Name = name

	return &tag, nil
}

// GetPageTags retrieves all tags for a page.
func (db *DB) GetPageTags(ctx context.Context, pageID int64) ([]models.Tag, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.id, t.name
		FROM tags t
		JOIN page_tags pt ON t.id = pt.tag_id
		WHERE pt.page_id = ?
		ORDER BY t.name
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}

	return tags, rows.Err()
}

// ListTags retrieves all tags with page counts.
func (db *DB) ListTags(ctx context.Context) ([]models.Tag, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.id, t.name, COUNT(pt.page_id) as page_count
		FROM tags t
		LEFT JOIN page_tags pt ON t.id = pt.tag_id
		GROUP BY t.id
		ORDER BY t.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.PageCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}

	return tags, rows.Err()
}

// Search queries

// sanitizeFTS5Query converts a user search query to a valid FTS5 query.
// It escapes special characters and adds prefix matching for better results.
func sanitizeFTS5Query(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	// Split into words and process each
	words := strings.Fields(query)
	var parts []string

	for _, word := range words {
		// Remove FTS5 special characters that could break the query
		word = strings.Map(func(r rune) rune {
			switch r {
			case '"', '\'', '*', '(', ')', ':', '^', '-', '+', '.':
				return -1 // Remove the character
			default:
				return r
			}
		}, word)

		word = strings.TrimSpace(word)
		if word != "" {
			// Add prefix matching with * for partial word matches
			parts = append(parts, word+"*")
		}
	}

	if len(parts) == 0 {
		return ""
	}

	// Join with OR for more flexible matching
	return strings.Join(parts, " OR ")
}

// SearchPages performs full-text search on pages.
func (db *DB) SearchPages(ctx context.Context, query string, limit int) ([]models.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Always use LIKE search for reliability - FTS5 can be tricky with SQLite
	return db.searchPagesLike(ctx, query, limit)
}

// searchPagesLike performs a fallback LIKE-based search when FTS5 fails or returns no results.
func (db *DB) searchPagesLike(ctx context.Context, query string, limit int) ([]models.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Create LIKE pattern
	likePattern := "%" + query + "%"

	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.title,
			   CASE
				   WHEN p.content LIKE ? THEN substr(p.content, 1, 150) || '...'
				   ELSE ''
			   END as snippet,
			   0.0 as rank, p.updated_at
		FROM pages p
		WHERE (p.title LIKE ? OR p.content LIKE ?)
		AND p.is_published = 1
		ORDER BY p.updated_at DESC
		LIMIT ?
	`, likePattern, likePattern, likePattern, limit)
	if err != nil {
		return nil, fmt.Errorf("fallback search failed: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var r models.SearchResult
		if err := rows.Scan(&r.PageID, &r.Slug, &r.Title, &r.Snippet, &r.Rank, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

// RebuildFTSIndex rebuilds the full-text search index from existing pages.
func (db *DB) RebuildFTSIndex(ctx context.Context) error {
	// Delete all entries from FTS table
	if _, err := db.ExecContext(ctx, "DELETE FROM pages_fts"); err != nil {
		return fmt.Errorf("failed to clear FTS index: %w", err)
	}

	// Repopulate from pages table
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pages_fts(rowid, title, content)
		SELECT id, title, content FROM pages
	`); err != nil {
		return fmt.Errorf("failed to rebuild FTS index: %w", err)
	}

	return nil
}

// Attachment queries

// CreateAttachment saves a new attachment.
func (db *DB) CreateAttachment(ctx context.Context, att *models.Attachment) error {
	att.CreatedAt = time.Now().UTC()

	result, err := db.ExecContext(ctx, `
		INSERT INTO attachments (page_id, filename, filepath, mime_type, size_bytes, uploader_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, att.PageID, att.Filename, att.Filepath, att.MimeType, att.SizeBytes, att.UploaderID, att.CreatedAt)
	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	att.ID = id

	return nil
}

// GetAttachment retrieves an attachment by ID.
func (db *DB) GetAttachment(ctx context.Context, id int64) (*models.Attachment, error) {
	att := &models.Attachment{}
	err := db.QueryRowContext(ctx, `
		SELECT id, page_id, filename, filepath, mime_type, size_bytes, uploader_id, created_at
		FROM attachments WHERE id = ?
	`, id).Scan(&att.ID, &att.PageID, &att.Filename, &att.Filepath, &att.MimeType, &att.SizeBytes, &att.UploaderID, &att.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return att, err
}

// ListAttachments retrieves attachments for a page.
func (db *DB) ListAttachments(ctx context.Context, pageID int64) ([]models.Attachment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, page_id, filename, filepath, mime_type, size_bytes, uploader_id, created_at
		FROM attachments WHERE page_id = ?
		ORDER BY created_at DESC
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []models.Attachment
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.ID, &a.PageID, &a.Filename, &a.Filepath, &a.MimeType, &a.SizeBytes, &a.UploaderID, &a.CreatedAt); err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}

	return attachments, rows.Err()
}

// DeleteAttachment removes an attachment.
func (db *DB) DeleteAttachment(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM attachments WHERE id = ?", id)
	return err
}

// Audit log

// LogAudit records an audit event.
func (db *DB) LogAudit(ctx context.Context, userID *int64, action, entityType string, entityID *int64, details, ipAddress string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO audit_log (user_id, action, entity_type, entity_id, details, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, userID, action, entityType, entityID, details, ipAddress, time.Now().UTC())
	return err
}

// CountUsers returns the total number of users.
func (db *DB) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// CountPages returns the total number of pages.
func (db *DB) CountPages(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pages").Scan(&count)
	return count, err
}

// API Token queries

// CreateAPIToken inserts a new API token.
func (db *DB) CreateAPIToken(ctx context.Context, token *models.APIToken) error {
	token.CreatedAt = time.Now().UTC()

	result, err := db.ExecContext(ctx, `
		INSERT INTO api_tokens (user_id, token_hash, name, scopes, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, token.UserID, token.TokenHash, token.Name, token.Scopes, token.ExpiresAt, token.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create API token: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get token ID: %w", err)
	}

	token.ID = id
	return nil
}

// GetAPITokenByHash retrieves a token by its hash.
func (db *DB) GetAPITokenByHash(ctx context.Context, tokenHash string) (*models.APIToken, error) {
	token := &models.APIToken{}
	err := db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, name, scopes, last_used_at, expires_at, created_at
		FROM api_tokens WHERE token_hash = ?
	`, tokenHash).Scan(
		&token.ID, &token.UserID, &token.TokenHash, &token.Name,
		&token.Scopes, &token.LastUsedAt, &token.ExpiresAt, &token.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API token: %w", err)
	}
	return token, nil
}

// UpdateAPITokenLastUsed updates the last_used_at timestamp.
func (db *DB) UpdateAPITokenLastUsed(ctx context.Context, tokenID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE api_tokens SET last_used_at = ? WHERE id = ?
	`, time.Now().UTC(), tokenID)
	return err
}

// ListAPITokensByUser retrieves all tokens for a user.
func (db *DB) ListAPITokensByUser(ctx context.Context, userID int64) ([]models.APIToken, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, token_hash, name, scopes, last_used_at, expires_at, created_at
		FROM api_tokens WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list API tokens: %w", err)
	}
	defer rows.Close()

	var tokens []models.APIToken
	for rows.Next() {
		var t models.APIToken
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.TokenHash, &t.Name,
			&t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan API token: %w", err)
		}
		tokens = append(tokens, t)
	}

	return tokens, rows.Err()
}

// DeleteAPIToken removes an API token.
func (db *DB) DeleteAPIToken(ctx context.Context, tokenID int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM api_tokens WHERE id = ?", tokenID)
	return err
}

// DeleteExpiredAPITokens removes all expired tokens.
func (db *DB) DeleteExpiredAPITokens(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "DELETE FROM api_tokens WHERE expires_at < ?", time.Now().UTC())
	return err
}

// Settings queries

// GetSetting retrieves a setting by key.
func (db *DB) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetSetting creates or updates a setting.
func (db *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, time.Now().UTC())
	return err
}

// PageTreeNode represents a page in the navigation tree.
type PageTreeNode struct {
	ID       int64
	Slug     string
	Title    string
	Children []*PageTreeNode
}

// GetPageTree retrieves the full page tree for navigation.
func (db *DB) GetPageTree(ctx context.Context) ([]*PageTreeNode, error) {
	// Get all pages with parent info
	rows, err := db.QueryContext(ctx, `
		SELECT id, slug, title, parent_id
		FROM pages
		WHERE is_published = 1
		ORDER BY title ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get pages: %w", err)
	}
	defer rows.Close()

	// Build maps for tree construction
	nodes := make(map[int64]*PageTreeNode)
	children := make(map[int64][]int64) // parent_id -> child_ids
	var rootIDs []int64

	for rows.Next() {
		var id int64
		var slug, title string
		var parentID *int64

		if err := rows.Scan(&id, &slug, &title, &parentID); err != nil {
			return nil, fmt.Errorf("failed to scan page: %w", err)
		}

		nodes[id] = &PageTreeNode{
			ID:       id,
			Slug:     slug,
			Title:    title,
			Children: []*PageTreeNode{},
		}

		if parentID == nil {
			rootIDs = append(rootIDs, id)
		} else {
			children[*parentID] = append(children[*parentID], id)
		}
	}

	// Build tree recursively
	var buildTree func(id int64) *PageTreeNode
	buildTree = func(id int64) *PageTreeNode {
		node := nodes[id]
		for _, childID := range children[id] {
			node.Children = append(node.Children, buildTree(childID))
		}
		return node
	}

	var tree []*PageTreeNode
	for _, id := range rootIDs {
		tree = append(tree, buildTree(id))
	}

	return tree, rows.Err()
}

// GetAllSettings retrieves all settings as a map.
func (db *DB) GetAllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}

	return settings, rows.Err()
}

// Share Link queries

// CreateShareLink inserts a new share link.
func (db *DB) CreateShareLink(ctx context.Context, link *models.ShareLink) error {
	link.CreatedAt = time.Now().UTC()

	result, err := db.ExecContext(ctx, `
		INSERT INTO share_links (token_hash, page_id, created_by, include_children, max_views, max_ips, expires_at, is_revoked, view_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, link.TokenHash, link.PageID, link.CreatedBy, link.IncludeChildren, link.MaxViews, link.MaxIPs, link.ExpiresAt, link.IsRevoked, link.ViewCount, link.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create share link: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get share link ID: %w", err)
	}

	link.ID = id
	return nil
}

// GetShareLinkByToken retrieves a share link by its token hash.
func (db *DB) GetShareLinkByToken(ctx context.Context, tokenHash string) (*models.ShareLink, error) {
	link := &models.ShareLink{}
	err := db.QueryRowContext(ctx, `
		SELECT sl.id, sl.token_hash, sl.page_id, sl.created_by, sl.include_children,
		       sl.max_views, sl.max_ips, sl.expires_at, sl.is_revoked, sl.view_count, sl.created_at,
		       p.title, p.slug, u.username
		FROM share_links sl
		JOIN pages p ON sl.page_id = p.id
		JOIN users u ON sl.created_by = u.id
		WHERE sl.token_hash = ?
	`, tokenHash).Scan(
		&link.ID, &link.TokenHash, &link.PageID, &link.CreatedBy, &link.IncludeChildren,
		&link.MaxViews, &link.MaxIPs, &link.ExpiresAt, &link.IsRevoked, &link.ViewCount, &link.CreatedAt,
		&link.PageTitle, &link.PageSlug, &link.CreatorUsername,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get share link: %w", err)
	}
	return link, nil
}

// GetShareLinkByID retrieves a share link by ID.
func (db *DB) GetShareLinkByID(ctx context.Context, id int64) (*models.ShareLink, error) {
	link := &models.ShareLink{}
	err := db.QueryRowContext(ctx, `
		SELECT sl.id, sl.token_hash, sl.page_id, sl.created_by, sl.include_children,
		       sl.max_views, sl.max_ips, sl.expires_at, sl.is_revoked, sl.view_count, sl.created_at,
		       p.title, p.slug, u.username
		FROM share_links sl
		JOIN pages p ON sl.page_id = p.id
		JOIN users u ON sl.created_by = u.id
		WHERE sl.id = ?
	`, id).Scan(
		&link.ID, &link.TokenHash, &link.PageID, &link.CreatedBy, &link.IncludeChildren,
		&link.MaxViews, &link.MaxIPs, &link.ExpiresAt, &link.IsRevoked, &link.ViewCount, &link.CreatedAt,
		&link.PageTitle, &link.PageSlug, &link.CreatorUsername,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get share link: %w", err)
	}

	// Get unique IP count
	uniqueIPs, err := db.GetShareLinkUniqueIPCount(ctx, id)
	if err != nil {
		return nil, err
	}
	link.UniqueIPs = uniqueIPs

	return link, nil
}

// GetShareLinksByPage retrieves all share links for a specific page.
func (db *DB) GetShareLinksByPage(ctx context.Context, pageID int64) ([]models.ShareLink, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT sl.id, sl.token_hash, sl.page_id, sl.created_by, sl.include_children,
		       sl.max_views, sl.max_ips, sl.expires_at, sl.is_revoked, sl.view_count, sl.created_at,
		       p.title, p.slug, u.username
		FROM share_links sl
		JOIN pages p ON sl.page_id = p.id
		JOIN users u ON sl.created_by = u.id
		WHERE sl.page_id = ?
		ORDER BY sl.created_at DESC
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get share links: %w", err)
	}
	defer rows.Close()

	return db.scanShareLinks(ctx, rows)
}

// GetShareLinksByUser retrieves all share links created by a specific user.
func (db *DB) GetShareLinksByUser(ctx context.Context, userID int64) ([]models.ShareLink, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT sl.id, sl.token_hash, sl.page_id, sl.created_by, sl.include_children,
		       sl.max_views, sl.max_ips, sl.expires_at, sl.is_revoked, sl.view_count, sl.created_at,
		       p.title, p.slug, u.username
		FROM share_links sl
		JOIN pages p ON sl.page_id = p.id
		JOIN users u ON sl.created_by = u.id
		WHERE sl.created_by = ?
		ORDER BY sl.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get share links: %w", err)
	}
	defer rows.Close()

	return db.scanShareLinks(ctx, rows)
}

// ListAllShareLinks retrieves all share links (admin view).
func (db *DB) ListAllShareLinks(ctx context.Context, limit, offset int) ([]models.ShareLink, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT sl.id, sl.token_hash, sl.page_id, sl.created_by, sl.include_children,
		       sl.max_views, sl.max_ips, sl.expires_at, sl.is_revoked, sl.view_count, sl.created_at,
		       p.title, p.slug, u.username
		FROM share_links sl
		JOIN pages p ON sl.page_id = p.id
		JOIN users u ON sl.created_by = u.id
		ORDER BY sl.created_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list share links: %w", err)
	}
	defer rows.Close()

	return db.scanShareLinks(ctx, rows)
}

// scanShareLinks is a helper to scan share link rows.
func (db *DB) scanShareLinks(ctx context.Context, rows *sql.Rows) ([]models.ShareLink, error) {
	var links []models.ShareLink
	for rows.Next() {
		var link models.ShareLink
		if err := rows.Scan(
			&link.ID, &link.TokenHash, &link.PageID, &link.CreatedBy, &link.IncludeChildren,
			&link.MaxViews, &link.MaxIPs, &link.ExpiresAt, &link.IsRevoked, &link.ViewCount, &link.CreatedAt,
			&link.PageTitle, &link.PageSlug, &link.CreatorUsername,
		); err != nil {
			return nil, fmt.Errorf("failed to scan share link: %w", err)
		}
		links = append(links, link)
	}

	// Fetch unique IP counts for all links
	for i := range links {
		count, err := db.GetShareLinkUniqueIPCount(ctx, links[i].ID)
		if err != nil {
			return nil, err
		}
		links[i].UniqueIPs = count
	}

	return links, rows.Err()
}

// IncrementShareLinkViewCount atomically increments the view count.
func (db *DB) IncrementShareLinkViewCount(ctx context.Context, linkID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE share_links SET view_count = view_count + 1 WHERE id = ?
	`, linkID)
	return err
}

// RevokeShareLink marks a share link as revoked.
func (db *DB) RevokeShareLink(ctx context.Context, linkID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE share_links SET is_revoked = 1 WHERE id = ?
	`, linkID)
	return err
}

// DeleteShareLink removes a share link and its access records.
func (db *DB) DeleteShareLink(ctx context.Context, linkID int64) error {
	// Access records are deleted automatically via ON DELETE CASCADE
	_, err := db.ExecContext(ctx, "DELETE FROM share_links WHERE id = ?", linkID)
	return err
}

// RecordShareAccess logs an access to a share link.
func (db *DB) RecordShareAccess(ctx context.Context, access *models.ShareLinkAccess) error {
	access.AccessedAt = time.Now().UTC()

	result, err := db.ExecContext(ctx, `
		INSERT INTO share_link_access (share_link_id, ip_address, user_agent, accessed_at)
		VALUES (?, ?, ?, ?)
	`, access.ShareLinkID, access.IPAddress, access.UserAgent, access.AccessedAt)
	if err != nil {
		return fmt.Errorf("failed to record share access: %w", err)
	}

	id, _ := result.LastInsertId()
	access.ID = id
	return nil
}

// GetShareLinkUniqueIPCount returns the count of unique IPs that accessed a share link.
func (db *DB) GetShareLinkUniqueIPCount(ctx context.Context, linkID int64) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT ip_address) FROM share_link_access WHERE share_link_id = ?
	`, linkID).Scan(&count)
	return count, err
}

// HasIPAccessedShareLink checks if a specific IP has already accessed a share link.
func (db *DB) HasIPAccessedShareLink(ctx context.Context, linkID int64, ipAddress string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM share_link_access WHERE share_link_id = ? AND ip_address = ?
	`, linkID, ipAddress).Scan(&count)
	return count > 0, err
}

// GetShareLinkAccesses retrieves access records for a share link (for stats view).
func (db *DB) GetShareLinkAccesses(ctx context.Context, linkID int64, limit, offset int) ([]models.ShareLinkAccess, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, share_link_id, ip_address, user_agent, accessed_at
		FROM share_link_access
		WHERE share_link_id = ?
		ORDER BY accessed_at DESC
		LIMIT ? OFFSET ?
	`, linkID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get share accesses: %w", err)
	}
	defer rows.Close()

	var accesses []models.ShareLinkAccess
	for rows.Next() {
		var a models.ShareLinkAccess
		if err := rows.Scan(&a.ID, &a.ShareLinkID, &a.IPAddress, &a.UserAgent, &a.AccessedAt); err != nil {
			return nil, fmt.Errorf("failed to scan share access: %w", err)
		}
		accesses = append(accesses, a)
	}

	return accesses, rows.Err()
}

// CountShareLinks returns the total number of share links.
func (db *DB) CountShareLinks(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM share_links").Scan(&count)
	return count, err
}

// IsPageDescendant checks if childSlug is a descendant of the page with parentID.
func (db *DB) IsPageDescendant(ctx context.Context, parentID int64, childSlug string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		WITH RECURSIVE descendants AS (
			SELECT id, slug FROM pages WHERE parent_id = ?
			UNION ALL
			SELECT p.id, p.slug FROM pages p
			JOIN descendants d ON p.parent_id = d.id
		)
		SELECT COUNT(*) FROM descendants WHERE slug = ? COLLATE NOCASE
	`, parentID, childSlug).Scan(&count)
	return count > 0, err
}
