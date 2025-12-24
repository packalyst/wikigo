package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"strings"

	"github.com/labstack/echo/v4"

	"gowiki/internal/database"
	"gowiki/internal/models"
)

// Share context keys
const (
	shareTokenContextKey   contextKey = "shareToken"
	shareLinkContextKey    contextKey = "shareLink"
	shareAccessedContextKey contextKey = "shareAccessed"
)

// ShareContext holds share link information for the request
type ShareContext struct {
	Link            *models.ShareLink
	Token           string // The raw token (not hash) for URL generation
	IsValid         bool
	InvalidReason   string
	RequestedSlug   string // The slug being accessed
	IsChildAccess   bool   // True if accessing a child page via include_children
}

// ShareMiddleware validates share tokens and sets context for shared access.
// This middleware should be applied to routes that can be accessed via share links.
func ShareMiddleware(db *database.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract token from query parameter
			token := c.QueryParam("token")
			if token == "" {
				// No share token, continue normally
				return next(c)
			}

			// Validate token format (base64url, ~43 chars for 32 bytes)
			if !isValidTokenFormat(token) {
				// Invalid format, ignore and continue
				return next(c)
			}

			// Hash the token for database lookup (constant time not needed for hashing)
			tokenHash := hashToken(token)

			// Look up share link by hash
			ctx := c.Request().Context()
			link, err := db.GetShareLinkByToken(ctx, tokenHash)
			if err != nil {
				// Database error, log and continue without share access
				c.Logger().Errorf("share link lookup error: %v", err)
				return next(c)
			}

			if link == nil {
				// Token not found, continue without share access
				return next(c)
			}

			// Build share context
			shareCtx := &ShareContext{
				Link:  link,
				Token: token,
			}

			// Validate the share link
			if !validateShareLink(ctx, db, link, c, shareCtx) {
				// Store invalid context for error handling
				ctx = context.WithValue(ctx, shareLinkContextKey, shareCtx)
				c.SetRequest(c.Request().WithContext(ctx))
				return next(c)
			}

			shareCtx.IsValid = true

			// Store share context
			ctx = context.WithValue(ctx, shareLinkContextKey, shareCtx)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// validateShareLink checks all validity conditions for a share link
func validateShareLink(ctx context.Context, db *database.DB, link *models.ShareLink, c echo.Context, shareCtx *ShareContext) bool {
	// Check if revoked
	if link.IsRevoked {
		shareCtx.InvalidReason = "This share link has been revoked"
		return false
	}

	// Check expiration
	if link.IsExpired() {
		shareCtx.InvalidReason = "This share link has expired"
		return false
	}

	// Check view limit
	if link.IsViewLimitReached() {
		shareCtx.InvalidReason = "This share link has reached its view limit"
		return false
	}

	// Check IP limit
	if link.MaxIPs != nil {
		ipAddress := sanitizeIP(c.RealIP())
		uniqueIPs, err := db.GetShareLinkUniqueIPCount(ctx, link.ID)
		if err != nil {
			c.Logger().Errorf("failed to get unique IP count: %v", err)
			shareCtx.InvalidReason = "Unable to validate share link"
			return false
		}

		// Check if this is a new IP and would exceed limit
		hasAccessed, err := db.HasIPAccessedShareLink(ctx, link.ID, ipAddress)
		if err != nil {
			c.Logger().Errorf("failed to check IP access: %v", err)
			shareCtx.InvalidReason = "Unable to validate share link"
			return false
		}

		if !hasAccessed && uniqueIPs >= *link.MaxIPs {
			shareCtx.InvalidReason = "This share link has reached its access limit"
			return false
		}
	}

	return true
}

// RecordShareAccess records an access to a share link.
// Should be called after successfully rendering the shared page.
func RecordShareAccess(c echo.Context, db *database.DB) error {
	shareCtx := GetShareContext(c)
	if shareCtx == nil || !shareCtx.IsValid {
		return nil
	}

	// Check if already recorded this request
	if c.Request().Context().Value(shareAccessedContextKey) != nil {
		return nil
	}

	ctx := c.Request().Context()

	// Record the access
	access := &models.ShareLinkAccess{
		ShareLinkID: shareCtx.Link.ID,
		IPAddress:   sanitizeIP(c.RealIP()),
		UserAgent:   truncateUserAgent(c.Request().UserAgent()),
	}

	if err := db.RecordShareAccess(ctx, access); err != nil {
		c.Logger().Errorf("failed to record share access: %v", err)
		return err
	}

	// Increment view count
	if err := db.IncrementShareLinkViewCount(ctx, shareCtx.Link.ID); err != nil {
		c.Logger().Errorf("failed to increment view count: %v", err)
		return err
	}

	// Mark as recorded
	ctx = context.WithValue(ctx, shareAccessedContextKey, true)
	c.SetRequest(c.Request().WithContext(ctx))

	return nil
}

// GetShareContext retrieves the share context from the request.
func GetShareContext(c echo.Context) *ShareContext {
	ctx, ok := c.Request().Context().Value(shareLinkContextKey).(*ShareContext)
	if !ok {
		return nil
	}
	return ctx
}

// HasValidShareAccess returns true if the request has a valid share token.
func HasValidShareAccess(c echo.Context) bool {
	ctx := GetShareContext(c)
	return ctx != nil && ctx.IsValid
}

// CanAccessPageViaShare checks if a share token grants access to a specific page.
func CanAccessPageViaShare(c echo.Context, db *database.DB, pageSlug string) bool {
	shareCtx := GetShareContext(c)
	if shareCtx == nil || !shareCtx.IsValid {
		return false
	}

	// Direct match on the shared page
	if strings.EqualFold(shareCtx.Link.PageSlug, pageSlug) {
		return true
	}

	// If include_children is enabled, check if the requested page is a descendant
	if shareCtx.Link.IncludeChildren {
		isDescendant, err := db.IsPageDescendant(c.Request().Context(), shareCtx.Link.PageID, pageSlug)
		if err != nil {
			c.Logger().Errorf("failed to check page descendant: %v", err)
			return false
		}
		if isDescendant {
			shareCtx.IsChildAccess = true
			shareCtx.RequestedSlug = pageSlug
			return true
		}
	}

	return false
}

// hashToken creates a SHA-256 hash of a token for secure storage/lookup.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// HashToken is exported for use in handlers when creating share links.
func HashToken(token string) string {
	return hashToken(token)
}

// isValidTokenFormat checks if a token has a valid format.
// Tokens are base64url encoded 32 bytes = 43 characters.
func isValidTokenFormat(token string) bool {
	// Allow some flexibility in length (40-50 chars)
	if len(token) < 40 || len(token) > 50 {
		return false
	}

	// Check for valid base64url characters
	for _, r := range token {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}

	return true
}

// sanitizeIP extracts and sanitizes the IP address.
func sanitizeIP(ip string) string {
	// Handle IPv6 addresses with zone identifiers
	if strings.Contains(ip, "%") {
		ip = ip[:strings.Index(ip, "%")]
	}

	// Parse and re-format to normalize
	parsed := net.ParseIP(ip)
	if parsed == nil {
		// If parsing fails, return a sanitized version
		if len(ip) > 45 {
			ip = ip[:45]
		}
		return ip
	}

	return parsed.String()
}

// truncateUserAgent limits user agent length for storage.
func truncateUserAgent(ua string) string {
	const maxLen = 500
	if len(ua) > maxLen {
		return ua[:maxLen]
	}
	return ua
}

// ConstantTimeCompare performs a constant-time comparison of two strings.
// Used for token comparison to prevent timing attacks.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
