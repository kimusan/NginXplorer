// Package api provides the HTTP server, authentication, SSE streaming,
// and REST API handlers for NginXplorer's web dashboard.
package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// AuthManager handles session-based authentication for the web dashboard.
type AuthManager struct {
	mu       sync.RWMutex
	users    map[string]string // username -> bcrypt hash
	sessions map[string]*session
	enabled  bool
}

type session struct {
	username  string
	token     string
	createdAt time.Time
	expiresAt time.Time
}

// AuthConfig holds the auth configuration for the manager.
type AuthConfig struct {
	Enabled bool
	Users   []UserEntry
}

// UserEntry is a username/password hash pair.
type UserEntry struct {
	Username     string
	PasswordHash string
}

// NewAuthManager creates a new authentication manager.
func NewAuthManager(cfg AuthConfig) *AuthManager {
	am := &AuthManager{
		users:    make(map[string]string),
		sessions: make(map[string]*session),
		enabled:  cfg.Enabled,
	}

	for _, u := range cfg.Users {
		am.users[u.Username] = u.PasswordHash
	}

	// Start session cleanup goroutine
	go am.cleanupLoop()

	return am
}

// Authenticate checks a username/password combination and returns a session token.
func (am *AuthManager) Authenticate(username, password string) (string, error) {
	am.mu.RLock()
	hash, ok := am.users[username]
	am.mu.RUnlock()

	if !ok {
		// Prevent timing attacks by still running bcrypt
		bcrypt.CompareHashAndPassword([]byte("$2a$10$000000000000000000000000000000000000000000000000000000"), []byte(password))
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	// Generate session token
	token, err := generateToken()
	if err != nil {
		return "", err
	}

	am.mu.Lock()
	am.sessions[token] = &session{
		username:  username,
		token:     token,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(24 * time.Hour),
	}
	am.mu.Unlock()

	slog.Info("user authenticated", "username", username)
	return token, nil
}

// ValidateToken checks if a session token is valid.
func (am *AuthManager) ValidateToken(token string) bool {
	if !am.enabled {
		return true
	}

	am.mu.RLock()
	defer am.mu.RUnlock()

	sess, ok := am.sessions[token]
	if !ok {
		return false
	}

	return time.Now().Before(sess.expiresAt)
}

// IsEnabled returns whether authentication is enabled.
func (am *AuthManager) IsEnabled() bool {
	return am.enabled
}

// cleanupLoop removes expired sessions every 5 minutes.
func (am *AuthManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		am.mu.Lock()
		now := time.Now()
		for token, sess := range am.sessions {
			if now.After(sess.expiresAt) {
				delete(am.sessions, token)
			}
		}
		am.mu.Unlock()
	}
}

// generateToken creates a cryptographically secure random token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Middleware returns an HTTP middleware that enforces authentication.
func (am *AuthManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !am.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for login endpoint
		if r.URL.Path == "/api/v1/auth/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie first
		token := ""
		if cookie, err := r.Cookie("nginxplorer_session"); err == nil {
			token = cookie.Value
		}

		// Fall back to query parameter (for SSE EventSource which can't set headers)
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		// Fall back to Authorization header (Bearer token)
		if token == "" {
			auth := r.Header.Get("Authorization")
			if len(auth) > 7 && auth[:7] == "Bearer " {
				token = auth[7:]
			}
		}

		if !am.ValidateToken(token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "unauthorized",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HandleLogin processes login requests.
func (am *AuthManager) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	token, err := am.Authenticate(req.Username, req.Password)
	if err != nil {
		slog.Warn("login failed", "username", req.Username, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid credentials",
		})
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "nginxplorer_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24 hours
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": token,
	})
}

// HandleAuthCheck returns 200 if authenticated, 401 if not.
func (am *AuthManager) HandleAuthCheck(w http.ResponseWriter, r *http.Request) {
	// If we reach here, the auth middleware already validated the token
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "authenticated",
	})
}

// ErrInvalidCredentials is returned when login credentials are wrong.
var ErrInvalidCredentials = &AuthError{Message: "invalid credentials"}

// AuthError represents an authentication error.
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

// HashPassword generates a bcrypt hash for a plaintext password.
// Used by the `nginxplorer hash-password` CLI command.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// ConstantTimeCompare compares two strings in constant time.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
