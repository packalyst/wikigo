# GoWiki

A modern, lightweight, self-hosted wiki built with Go. Designed for simplicity, security, and performance.

## Features

- **Fast & Lightweight**: Single binary, ~20MB Docker image, minimal resource usage
- **Markdown Support**: Full GitHub Flavored Markdown with live preview
- **Wiki Links**: `[[Page Name]]` syntax for internal linking
- **Full-Text Search**: SQLite FTS5 for instant search results
- **Version History**: Track all changes with revision history and revert
- **User Management**: Role-based access control (Admin, Editor, Viewer)
- **Hierarchical Pages**: Organize pages in nested folder structures
- **Auto Backup**: Automatic markdown file backup with git-friendly structure
- **Modern UI**: Clean, responsive design with dark mode support
- **Docker Ready**: Simple deployment with Docker Compose
- **Secure**: CSRF protection, rate limiting, secure sessions, bcrypt passwords

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend | Go 1.22+, Echo v4 |
| Templates | Templ (type-safe) |
| Database | SQLite + FTS5 |
| Markdown | Goldmark |
| Frontend | Tailwind CSS v4, HTMX, Alpine.js |
| Container | Docker (Alpine) |

## Quick Start

### Using Docker Compose (Recommended)

```bash
cd gowiki

# Create environment file
cp .env.example .env

# Generate a secret key
echo "WIKI_SECRET_KEY=$(openssl rand -hex 32)" >> .env

# Start the wiki
docker-compose up -d

# View logs
docker-compose logs -f
```

Access the wiki at http://localhost:8080. On first run, you'll see a setup page to create your admin account.

### Manual Installation

```bash
# Prerequisites: Go 1.22+, Node.js (for CSS)

# Enter project directory
cd gowiki

# Install dependencies and build
make deps
make build

# Run
./gowiki
```

## Configuration

All configuration is done via environment variables:

### Core Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `WIKI_SECRET_KEY` | (auto-generated) | Session encryption key (32+ chars) |
| `WIKI_PORT` | `8080` | Server port |
| `WIKI_HOST` | `0.0.0.0` | Host to bind |
| `WIKI_SITE_NAME` | `GoWiki` | Site title |
| `WIKI_SITE_URL` | `http://localhost:8080` | Public URL |

### User & Registration

| Variable | Default | Description |
|----------|---------|-------------|
| `WIKI_ALLOW_REGISTRATION` | `false` | Enable public registration |
| `WIKI_DEFAULT_ROLE` | `viewer` | Role for new users (admin/editor/viewer) |

### Database & Storage

| Variable | Default | Description |
|----------|---------|-------------|
| `WIKI_DB_PATH` | `./data/wiki.db` | Database file path |
| `WIKI_UPLOAD_PATH` | `./uploads` | Upload directory |
| `WIKI_MAX_UPLOAD_SIZE` | `10485760` | Max upload size (10MB) |
| `WIKI_BACKUP_ENABLED` | `true` | Enable markdown file backups |
| `WIKI_BACKUP_PATH` | `./backups` | Backup directory |

### Security

| Variable | Default | Description |
|----------|---------|-------------|
| `WIKI_BCRYPT_COST` | `12` | Password hashing cost (10-31) |
| `WIKI_RATE_LIMIT` | `100` | Requests per minute |
| `WIKI_SESSION_MAX_AGE` | `604800` | Session duration (7 days in seconds) |

See `.env.example` for all options.

## Development

```bash
# Install development tools
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/cosmtrek/air@latest

# Run with live reload
make dev

# In another terminal, watch CSS changes
make css-watch

# Run tests
make test

# Format code
make fmt
```

## Project Structure

```
gowiki/
├── cmd/wiki/           # Application entry point
├── internal/
│   ├── config/         # Configuration management
│   ├── database/       # SQLite database & migrations
│   ├── handlers/       # HTTP request handlers
│   ├── middleware/     # Auth, CSRF, security
│   ├── models/         # Data structures
│   ├── services/       # Business logic
│   └── views/          # Templ templates
├── static/             # CSS, JS assets
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

## API

GoWiki includes a REST API for programmatic access. See [API.md](API.md) for full documentation.

**Quick example:**
```bash
# Create an API token from User Menu → API Tokens, then:
curl -H "Authorization: Bearer YOUR_TOKEN" \
  https://your-wiki.com/api/v1/pages
```

API features:
- Bearer token authentication
- Full CRUD for pages
- Search, tags, user management
- Rate limiting

## Security

- Passwords hashed with bcrypt (cost 12)
- Session cookies: HttpOnly, Secure, SameSite
- CSRF protection on all state-changing requests
- Rate limiting on login attempts
- SQL injection prevention (parameterized queries)
- XSS protection (HTML sanitization with bluemonday)
- Security headers (CSP, X-Frame-Options, etc.)
- Non-root Docker container

## Backup

### Automatic Markdown Backup

GoWiki automatically saves all pages as markdown files in the `backups/` directory. This creates a git-friendly folder structure:

```
backups/
├── linux/
│   └── ubuntu/
│       └── networking.md
├── getting-started.md
└── home.md
```

Each file includes YAML front matter with metadata (title, author, date). You can version this directory with git.

### Database Backup

For a complete backup including the database:

```bash
# Stop the wiki
docker-compose stop

# Backup data
tar -czvf wiki-backup-$(date +%Y%m%d).tar.gz data/ uploads/ backups/

# Restart
docker-compose start
```

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
