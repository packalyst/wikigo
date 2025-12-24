package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Migration represents a database schema migration.
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// migrations contains all database migrations in order.
var migrations = []Migration{
	{
		Version:     1,
		Description: "Create users table",
		SQL: `
			CREATE TABLE IF NOT EXISTS users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				username TEXT UNIQUE NOT NULL COLLATE NOCASE,
				email TEXT UNIQUE NOT NULL COLLATE NOCASE,
				password_hash TEXT NOT NULL,
				role TEXT NOT NULL DEFAULT 'viewer' CHECK(role IN ('admin', 'editor', 'viewer')),
				is_active INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_login_at DATETIME
			);

			CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
			CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
		`,
	},
	{
		Version:     2,
		Description: "Create pages table",
		SQL: `
			CREATE TABLE IF NOT EXISTS pages (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				slug TEXT UNIQUE NOT NULL COLLATE NOCASE,
				title TEXT NOT NULL,
				content TEXT NOT NULL DEFAULT '',
				content_html TEXT NOT NULL DEFAULT '',
				author_id INTEGER NOT NULL REFERENCES users(id),
				is_published INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				published_at DATETIME
			);

			CREATE INDEX IF NOT EXISTS idx_pages_slug ON pages(slug);
			CREATE INDEX IF NOT EXISTS idx_pages_author ON pages(author_id);
			CREATE INDEX IF NOT EXISTS idx_pages_updated ON pages(updated_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pages_published ON pages(is_published, published_at DESC);
		`,
	},
	{
		Version:     3,
		Description: "Create revisions table",
		SQL: `
			CREATE TABLE IF NOT EXISTS revisions (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
				content TEXT NOT NULL,
				author_id INTEGER NOT NULL REFERENCES users(id),
				comment TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_revisions_page ON revisions(page_id, created_at DESC);
		`,
	},
	{
		Version:     4,
		Description: "Create tags table",
		SQL: `
			CREATE TABLE IF NOT EXISTS tags (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT UNIQUE NOT NULL COLLATE NOCASE
			);

			CREATE TABLE IF NOT EXISTS page_tags (
				page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
				tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
				PRIMARY KEY (page_id, tag_id)
			);

			CREATE INDEX IF NOT EXISTS idx_page_tags_tag ON page_tags(tag_id);
		`,
	},
	{
		Version:     5,
		Description: "Create attachments table",
		SQL: `
			CREATE TABLE IF NOT EXISTS attachments (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				page_id INTEGER REFERENCES pages(id) ON DELETE SET NULL,
				filename TEXT NOT NULL,
				filepath TEXT NOT NULL,
				mime_type TEXT NOT NULL,
				size_bytes INTEGER NOT NULL,
				uploader_id INTEGER NOT NULL REFERENCES users(id),
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_attachments_page ON attachments(page_id);
		`,
	},
	{
		Version:     6,
		Description: "Create full-text search index",
		SQL: `
			CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
				title,
				content,
				content='pages',
				content_rowid='id',
				tokenize='porter unicode61'
			);

			-- Triggers to keep FTS index synchronized
			CREATE TRIGGER IF NOT EXISTS pages_fts_insert AFTER INSERT ON pages BEGIN
				INSERT INTO pages_fts(rowid, title, content)
				VALUES (new.id, new.title, new.content);
			END;

			CREATE TRIGGER IF NOT EXISTS pages_fts_delete AFTER DELETE ON pages BEGIN
				INSERT INTO pages_fts(pages_fts, rowid, title, content)
				VALUES('delete', old.id, old.title, old.content);
			END;

			CREATE TRIGGER IF NOT EXISTS pages_fts_update AFTER UPDATE ON pages BEGIN
				INSERT INTO pages_fts(pages_fts, rowid, title, content)
				VALUES('delete', old.id, old.title, old.content);
				INSERT INTO pages_fts(rowid, title, content)
				VALUES (new.id, new.title, new.content);
			END;
		`,
	},
	{
		Version:     7,
		Description: "Create sessions table",
		SQL: `
			CREATE TABLE IF NOT EXISTS sessions (
				id TEXT PRIMARY KEY,
				user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				data TEXT NOT NULL DEFAULT '{}',
				ip_address TEXT NOT NULL DEFAULT '',
				user_agent TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				expires_at DATETIME NOT NULL
			);

			CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
			CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
		`,
	},
	{
		Version:     8,
		Description: "Create audit log table",
		SQL: `
			CREATE TABLE IF NOT EXISTS audit_log (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
				action TEXT NOT NULL,
				entity_type TEXT NOT NULL,
				entity_id INTEGER,
				details TEXT,
				ip_address TEXT,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_log(user_id);
			CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_log(entity_type, entity_id);
			CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at DESC);
		`,
	},
	{
		Version:     9,
		Description: "Create API tokens table",
		SQL: `
			CREATE TABLE IF NOT EXISTS api_tokens (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				token_hash TEXT UNIQUE NOT NULL,
				name TEXT NOT NULL DEFAULT '',
				scopes TEXT NOT NULL DEFAULT 'read',
				last_used_at DATETIME,
				expires_at DATETIME NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
			CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
			CREATE INDEX IF NOT EXISTS idx_api_tokens_expires ON api_tokens(expires_at);
		`,
	},
	{
		Version:     10,
		Description: "Create settings table",
		SQL: `
			CREATE TABLE IF NOT EXISTS settings (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		`,
	},
	{
		Version:     11,
		Description: "Add parent_id to pages for hierarchical structure",
		SQL: `
			ALTER TABLE pages ADD COLUMN parent_id INTEGER REFERENCES pages(id) ON DELETE SET NULL;
			CREATE INDEX IF NOT EXISTS idx_pages_parent ON pages(parent_id);
		`,
	},
	{
		Version:     12,
		Description: "Create share_links table for page sharing",
		SQL: `
			CREATE TABLE IF NOT EXISTS share_links (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				token_hash TEXT NOT NULL UNIQUE,
				page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
				created_by INTEGER NOT NULL REFERENCES users(id),
				include_children INTEGER NOT NULL DEFAULT 0,
				max_views INTEGER,
				max_ips INTEGER,
				expires_at DATETIME,
				is_revoked INTEGER NOT NULL DEFAULT 0,
				view_count INTEGER NOT NULL DEFAULT 0,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_share_links_token ON share_links(token_hash);
			CREATE INDEX IF NOT EXISTS idx_share_links_page ON share_links(page_id);
			CREATE INDEX IF NOT EXISTS idx_share_links_creator ON share_links(created_by);
		`,
	},
	{
		Version:     13,
		Description: "Create share_link_access table for access tracking",
		SQL: `
			CREATE TABLE IF NOT EXISTS share_link_access (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				share_link_id INTEGER NOT NULL REFERENCES share_links(id) ON DELETE CASCADE,
				ip_address TEXT NOT NULL,
				user_agent TEXT,
				accessed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_share_access_link ON share_link_access(share_link_id);
			CREATE INDEX IF NOT EXISTS idx_share_access_ip ON share_link_access(share_link_id, ip_address);
		`,
	},
}

// Migrate runs all pending migrations.
func (db *DB) Migrate(ctx context.Context) error {
	// Create migrations tracking table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	var currentVersion int
	err = db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("failed to get current migration version: %w", err)
	}

	// Apply pending migrations
	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		fmt.Printf("Applying migration %d: %s\n", m.Version, m.Description)

		err := db.Transaction(ctx, func(tx *sql.Tx) error {
			// Execute migration SQL
			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return fmt.Errorf("failed to execute migration SQL: %w", err)
			}

			// Record migration
			_, err := tx.ExecContext(ctx,
				"INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, ?)",
				m.Version, m.Description, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("failed to record migration: %w", err)
			}

			return nil
		})

		if err != nil {
			return fmt.Errorf("migration %d failed: %w", m.Version, err)
		}

		fmt.Printf("Migration %d applied successfully\n", m.Version)
	}

	return nil
}

// CurrentVersion returns the current schema version.
func (db *DB) CurrentVersion(ctx context.Context) (int, error) {
	var version int
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}
