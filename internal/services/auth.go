package services

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"gowiki/internal/config"
	"gowiki/internal/database"
	"gowiki/internal/models"
)

// Authentication errors.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserInactive       = errors.New("user account is inactive")
	ErrUserExists         = errors.New("username or email already exists")
	ErrInvalidPassword    = errors.New("password does not meet requirements")
	ErrInvalidUsername    = errors.New("username does not meet requirements")
	ErrInvalidEmail       = errors.New("invalid email address")
)

// AuthService handles user authentication and authorization.
type AuthService struct {
	db         *database.DB
	cfg        *config.Config
	bcryptCost int
}

// NewAuthService creates a new authentication service.
func NewAuthService(db *database.DB, cfg *config.Config) *AuthService {
	return &AuthService{
		db:         db,
		cfg:        cfg,
		bcryptCost: cfg.Security.BcryptCost,
	}
}

// Authenticate verifies user credentials and returns the user if valid.
func (s *AuthService) Authenticate(ctx context.Context, username, password string) (*models.User, error) {
	// Normalize username
	username = strings.TrimSpace(username)

	// Attempt to find user by username or email
	user, err := s.db.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("authentication error: %w", err)
	}

	if user == nil {
		// Try email
		user, err = s.db.GetUserByEmail(ctx, username)
		if err != nil {
			return nil, fmt.Errorf("authentication error: %w", err)
		}
	}

	// Perform constant-time comparison even if user doesn't exist
	// This prevents timing attacks
	if user == nil {
		// Hash a dummy password to prevent timing attacks
		bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy.hash.to.prevent.timing.attacks"), []byte(password))
		return nil, ErrInvalidCredentials
	}

	// Check if user is active
	if !user.IsActive {
		return nil, ErrUserInactive
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	// Update last login
	if err := s.db.UpdateUserLastLogin(ctx, user.ID); err != nil {
		// Log but don't fail authentication
		fmt.Printf("Warning: failed to update last login: %v\n", err)
	}

	return user, nil
}

// CreateUser creates a new user with validated input.
func (s *AuthService) CreateUser(ctx context.Context, input models.UserCreate) (*models.User, error) {
	// Validate username
	if err := s.ValidateUsername(input.Username); err != nil {
		return nil, err
	}

	// Validate email
	if err := s.ValidateEmail(input.Email); err != nil {
		return nil, err
	}

	// Validate password
	if err := s.ValidatePassword(input.Password); err != nil {
		return nil, err
	}

	// Validate role
	if !input.Role.IsValid() {
		input.Role = models.Role(s.cfg.Site.DefaultRole)
	}

	// Check if user already exists
	existing, _ := s.db.GetUserByUsername(ctx, input.Username)
	if existing != nil {
		return nil, ErrUserExists
	}

	existing, _ = s.db.GetUserByEmail(ctx, input.Email)
	if existing != nil {
		return nil, ErrUserExists
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), s.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now().UTC()
	user := &models.User{
		Username:     input.Username,
		Email:        strings.ToLower(input.Email),
		PasswordHash: string(hash),
		Role:         input.Role,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.db.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// ChangePassword changes a user's password.
func (s *AuthService) ChangePassword(ctx context.Context, userID int64, currentPassword, newPassword string) error {
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrUserNotFound
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	// Validate new password
	if err := s.ValidatePassword(newPassword); err != nil {
		return err
	}

	// Hash new password
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	hashStr := string(hash)
	return s.db.UpdateUser(ctx, userID, &models.UserUpdate{Password: &hashStr})
}

// ValidateUsername checks if a username meets requirements.
func (s *AuthService) ValidateUsername(username string) error {
	username = strings.TrimSpace(username)

	if len(username) < 3 {
		return fmt.Errorf("%w: username must be at least 3 characters", ErrInvalidUsername)
	}

	if len(username) > 32 {
		return fmt.Errorf("%w: username must be at most 32 characters", ErrInvalidUsername)
	}

	// Only allow alphanumeric, underscore, and hyphen
	validUsername := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	if !validUsername.MatchString(username) {
		return fmt.Errorf("%w: username must start with a letter and contain only letters, numbers, underscores, and hyphens", ErrInvalidUsername)
	}

	// Check for reserved usernames
	reserved := []string{"admin", "administrator", "root", "system", "api", "wiki", "help", "support"}
	lowerUsername := strings.ToLower(username)
	for _, r := range reserved {
		if lowerUsername == r {
			return fmt.Errorf("%w: username '%s' is reserved", ErrInvalidUsername, username)
		}
	}

	return nil
}

// ValidateEmail checks if an email is valid.
func (s *AuthService) ValidateEmail(email string) error {
	email = strings.TrimSpace(email)

	if len(email) < 5 || len(email) > 254 {
		return ErrInvalidEmail
	}

	// Basic email validation regex
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(email) {
		return ErrInvalidEmail
	}

	return nil
}

// ValidatePassword checks if a password meets security requirements.
func (s *AuthService) ValidatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("%w: password must be at least 8 characters", ErrInvalidPassword)
	}

	if len(password) > 72 {
		// bcrypt has a maximum length of 72 bytes
		return fmt.Errorf("%w: password must be at most 72 characters", ErrInvalidPassword)
	}

	var hasUpper, hasLower, hasDigit bool

	for _, c := range password {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasDigit = true
		}
	}

	if !hasUpper {
		return fmt.Errorf("%w: password must contain at least one uppercase letter", ErrInvalidPassword)
	}
	if !hasLower {
		return fmt.Errorf("%w: password must contain at least one lowercase letter", ErrInvalidPassword)
	}
	if !hasDigit {
		return fmt.Errorf("%w: password must contain at least one digit", ErrInvalidPassword)
	}

	// Check for common weak passwords
	weakPasswords := []string{
		"password", "12345678", "qwerty", "letmein", "welcome",
		"admin123", "password1", "Password1",
	}

	lowerPassword := strings.ToLower(password)
	for _, weak := range weakPasswords {
		if strings.Contains(lowerPassword, weak) {
			return fmt.Errorf("%w: password is too common", ErrInvalidPassword)
		}
	}

	return nil
}

// GenerateCSRFToken creates a cryptographically secure CSRF token.
func (s *AuthService) GenerateCSRFToken() (string, error) {
	bytes := make([]byte, s.cfg.Security.CSRFTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate CSRF token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// ValidateCSRFToken performs constant-time comparison of CSRF tokens.
func (s *AuthService) ValidateCSRFToken(expected, actual string) bool {
	if expected == "" || actual == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

// GetUserByID retrieves a user by ID.
func (s *AuthService) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	return s.db.GetUserByID(ctx, id)
}

// ListUsers retrieves all users.
func (s *AuthService) ListUsers(ctx context.Context, limit, offset int) ([]models.User, error) {
	return s.db.ListUsers(ctx, limit, offset)
}

// UpdateUser updates a user's details.
func (s *AuthService) UpdateUser(ctx context.Context, id int64, update *models.UserUpdate) error {
	// Validate email if provided
	if update.Email != nil {
		if err := s.ValidateEmail(*update.Email); err != nil {
			return err
		}
	}

	// Hash password if provided
	if update.Password != nil {
		if err := s.ValidatePassword(*update.Password); err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*update.Password), s.bcryptCost)
		if err != nil {
			return err
		}
		hashStr := string(hash)
		update.Password = &hashStr
	}

	return s.db.UpdateUser(ctx, id, update)
}

// DeleteUser removes a user.
func (s *AuthService) DeleteUser(ctx context.Context, id int64) error {
	return s.db.DeleteUser(ctx, id)
}

// HasAnyUsers checks if any users exist in the database.
func (s *AuthService) HasAnyUsers(ctx context.Context) (bool, error) {
	users, err := s.db.ListUsers(ctx, 1, 0)
	if err != nil {
		return false, err
	}
	return len(users) > 0, nil
}

// createUserInternal creates a user without username validation (for initial admin).
func (s *AuthService) createUserInternal(ctx context.Context, input models.UserCreate) (*models.User, error) {
	// Validate email
	if err := s.ValidateEmail(input.Email); err != nil {
		return nil, err
	}

	// Validate password
	if err := s.ValidatePassword(input.Password); err != nil {
		return nil, err
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), s.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Set default role if not specified
	role := input.Role
	if role == "" {
		role = models.RoleViewer
	}

	// Create user in database
	user := &models.User{
		Username:     strings.TrimSpace(input.Username),
		Email:        strings.ToLower(strings.TrimSpace(input.Email)),
		PasswordHash: string(hash),
		Role:         role,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := s.db.CreateUser(ctx, user); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// CreateInitialAdmin creates the first admin user during setup (bypasses reserved username check).
func (s *AuthService) CreateInitialAdmin(ctx context.Context, input models.UserCreate) (*models.User, error) {
	// Ensure this is only used when no users exist
	hasUsers, err := s.HasAnyUsers(ctx)
	if err != nil {
		return nil, err
	}
	if hasUsers {
		return nil, fmt.Errorf("initial admin can only be created when no users exist")
	}

	// Force admin role
	input.Role = models.RoleAdmin

	return s.createUserInternal(ctx, input)
}

// GetSetting retrieves a setting from the database.
func (s *AuthService) GetSetting(ctx context.Context, key string) (string, error) {
	return s.db.GetSetting(ctx, key)
}

// SetSetting stores a setting in the database.
func (s *AuthService) SetSetting(ctx context.Context, key, value string) error {
	return s.db.SetSetting(ctx, key, value)
}

// GetAllSettings retrieves all settings from the database.
func (s *AuthService) GetAllSettings(ctx context.Context) (map[string]string, error) {
	return s.db.GetAllSettings(ctx)
}
