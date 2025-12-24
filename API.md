# GoWiki API Documentation

GoWiki provides a REST API for programmatic access to wiki content. All API endpoints are prefixed with `/api/v1`.

## Authentication

The API supports two authentication methods:

### 1. API Tokens (Recommended)

Create an API token from the web UI (User Menu → API Tokens) or via the API, then use it in the Authorization header:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN_HERE" \
  https://your-wiki.com/api/v1/pages
```

### 2. JWT Tokens

For session-based authentication, login to get JWT tokens:

```bash
# Login
curl -X POST https://your-wiki.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "your-password"}'

# Response:
{
  "access_token": "eyJhbG...",
  "refresh_token": "eyJhbG...",
  "token_type": "Bearer",
  "expires_at": "2024-01-01T12:00:00Z"
}

# Use access token
curl -H "Authorization: Bearer eyJhbG..." \
  https://your-wiki.com/api/v1/me
```

## Response Format

### Success Response
```json
{
  "data": { ... }
}
```

### Paginated Response
```json
{
  "data": [ ... ],
  "total": 100,
  "limit": 20,
  "offset": 0
}
```

### Error Response
```json
{
  "error": "error message",
  "code": 400
}
```

---

## Endpoints

### Pages

#### List Pages
```http
GET /api/v1/pages
```

Query parameters:
| Parameter | Type | Description |
|-----------|------|-------------|
| `limit` | int | Results per page (1-100, default: 20) |
| `offset` | int | Skip N results |
| `tag` | string | Filter by tag |
| `order_by` | string | Sort field (updated_at, created_at, title) |
| `order_dir` | string | Sort direction (asc, desc) |

**Example:**
```bash
curl "https://your-wiki.com/api/v1/pages?limit=10&tag=tutorial"
```

#### Get Page
```http
GET /api/v1/pages/:slug
```

**Example:**
```bash
curl https://your-wiki.com/api/v1/pages/getting-started
```

**Response:**
```json
{
  "data": {
    "id": 1,
    "slug": "getting-started",
    "title": "Getting Started",
    "content": "# Welcome\n\nThis is the content...",
    "content_html": "<h1>Welcome</h1><p>This is the content...</p>",
    "author_id": 1,
    "is_published": true,
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T12:00:00Z",
    "tags": [{"id": 1, "name": "tutorial"}]
  }
}
```

#### Create Page
```http
POST /api/v1/pages
```
*Requires: Editor role*

**Request body:**
```json
{
  "title": "New Page Title",
  "slug": "new-page-title",
  "content": "# Content\n\nMarkdown content here...",
  "tags": ["tag1", "tag2"]
}
```

**Example:**
```bash
curl -X POST https://your-wiki.com/api/v1/pages \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "API Guide",
    "content": "# API Guide\n\nHow to use the API...",
    "tags": ["api", "documentation"]
  }'
```

#### Update Page
```http
PUT /api/v1/pages/:slug
```
*Requires: Editor role*

**Request body:** (all fields optional)
```json
{
  "title": "Updated Title",
  "content": "Updated content...",
  "tags": ["new-tag"],
  "is_published": true
}
```

**Example:**
```bash
curl -X PUT https://your-wiki.com/api/v1/pages/api-guide \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content": "# Updated Content\n\nNew content here..."}'
```

#### Delete Page
```http
DELETE /api/v1/pages/:slug
```
*Requires: Editor role*

**Example:**
```bash
curl -X DELETE https://your-wiki.com/api/v1/pages/old-page \
  -H "Authorization: Bearer YOUR_TOKEN"
```

---

### Tags

#### List Tags
```http
GET /api/v1/tags
```

**Example:**
```bash
curl https://your-wiki.com/api/v1/tags
```

**Response:**
```json
{
  "data": [
    {"id": 1, "name": "tutorial", "page_count": 5},
    {"id": 2, "name": "api", "page_count": 3}
  ]
}
```

#### Get Pages by Tag
```http
GET /api/v1/tags/:name
```

**Example:**
```bash
curl https://your-wiki.com/api/v1/tags/tutorial
```

---

### Search

#### Search Pages
```http
GET /api/v1/search?q=:query
```

Query parameters:
| Parameter | Type | Description |
|-----------|------|-------------|
| `q` | string | Search query (required) |
| `limit` | int | Max results (1-100, default: 20) |

**Example:**
```bash
curl "https://your-wiki.com/api/v1/search?q=getting+started&limit=10"
```

**Response:**
```json
{
  "data": [
    {
      "id": 1,
      "slug": "getting-started",
      "title": "Getting Started",
      "snippet": "...how to get <mark>started</mark> with...",
      "updated_at": "2024-01-01T12:00:00Z"
    }
  ]
}
```

---

### API Tokens

#### Create Token
```http
POST /api/v1/tokens
```
*Requires: Authentication*

**Request body:**
```json
{
  "name": "My Script",
  "scopes": "read,write"
}
```

Available scopes: `read`, `write`, `admin`

**Example:**
```bash
curl -X POST https://your-wiki.com/api/v1/tokens \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "CI/CD Pipeline", "scopes": "read,write"}'
```

**Response:**
```json
{
  "data": {
    "token": "abc123...",
    "token_info": {
      "id": 1,
      "name": "CI/CD Pipeline",
      "scopes": "read,write",
      "expires_at": "2025-01-01T00:00:00Z"
    }
  }
}
```

> **Important:** The `token` value is only shown once. Store it securely.

#### List Tokens
```http
GET /api/v1/tokens
```

#### Delete Token
```http
DELETE /api/v1/tokens/:id
```

---

### User

#### Get Current User
```http
GET /api/v1/me
```
*Requires: Authentication*

**Response:**
```json
{
  "data": {
    "id": 1,
    "username": "admin",
    "email": "admin@example.com",
    "role": "admin",
    "is_active": true
  }
}
```

#### List Users (Admin)
```http
GET /api/v1/admin/users
```
*Requires: Admin role*

---

### Authentication

#### Login
```http
POST /api/v1/auth/login
```

**Request body:**
```json
{
  "username": "admin",
  "password": "your-password"
}
```

**Response:**
```json
{
  "access_token": "eyJhbG...",
  "refresh_token": "eyJhbG...",
  "token_type": "Bearer",
  "expires_at": "2024-01-01T12:00:00Z"
}
```

#### Refresh Token
```http
POST /api/v1/auth/refresh
```
*Requires: Valid refresh token*

---

## Code Examples

### Python
```python
import requests

BASE_URL = "https://your-wiki.com/api/v1"
TOKEN = "your-api-token"

headers = {"Authorization": f"Bearer {TOKEN}"}

# List pages
response = requests.get(f"{BASE_URL}/pages", headers=headers)
pages = response.json()["data"]

# Create page
new_page = {
    "title": "New Page",
    "content": "# Hello World\n\nThis is content.",
    "tags": ["example"]
}
response = requests.post(f"{BASE_URL}/pages", json=new_page, headers=headers)

# Search
response = requests.get(f"{BASE_URL}/search", params={"q": "hello"}, headers=headers)
```

### JavaScript/Node.js
```javascript
const BASE_URL = 'https://your-wiki.com/api/v1';
const TOKEN = 'your-api-token';

const headers = {
  'Authorization': `Bearer ${TOKEN}`,
  'Content-Type': 'application/json'
};

// List pages
const pages = await fetch(`${BASE_URL}/pages`, { headers })
  .then(r => r.json());

// Create page
const newPage = await fetch(`${BASE_URL}/pages`, {
  method: 'POST',
  headers,
  body: JSON.stringify({
    title: 'New Page',
    content: '# Hello World\n\nThis is content.',
    tags: ['example']
  })
}).then(r => r.json());

// Search
const results = await fetch(`${BASE_URL}/search?q=hello`, { headers })
  .then(r => r.json());
```

### cURL
```bash
# Set your token
TOKEN="your-api-token"
BASE="https://your-wiki.com/api/v1"

# List all pages
curl -H "Authorization: Bearer $TOKEN" "$BASE/pages"

# Get specific page
curl -H "Authorization: Bearer $TOKEN" "$BASE/pages/getting-started"

# Create page
curl -X POST "$BASE/pages" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"New Page","content":"# Hello","tags":["test"]}'

# Update page
curl -X PUT "$BASE/pages/new-page" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"# Updated Content"}'

# Delete page
curl -X DELETE "$BASE/pages/old-page" \
  -H "Authorization: Bearer $TOKEN"

# Search
curl -H "Authorization: Bearer $TOKEN" "$BASE/search?q=hello"
```

---

## Rate Limiting

The API implements rate limiting to prevent abuse:
- **Login endpoint:** 5 attempts per 15 minutes per IP
- **General API:** 100 requests per minute per token

---

## HTTP Status Codes

| Code | Description |
|------|-------------|
| 200 | Success |
| 201 | Created |
| 204 | No Content (successful delete) |
| 400 | Bad Request (invalid input) |
| 401 | Unauthorized (invalid/missing token) |
| 403 | Forbidden (insufficient permissions) |
| 404 | Not Found |
| 409 | Conflict (e.g., slug already exists) |
| 429 | Too Many Requests (rate limited) |
| 500 | Internal Server Error |
