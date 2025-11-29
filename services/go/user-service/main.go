package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/bcrypt"
)

// Config holds the application configuration
type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	JWTExpiry   time.Duration
}

// User represents a user in the system
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// AuthResponse represents an authentication response
type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	User         *User  `json:"user"`
}

// Claims represents JWT claims
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Service handles user-related operations
type Service struct {
	config *Config
	db     *sql.DB
	router *mux.Router
}

func loadConfig() *Config {
	expiry := 24 * time.Hour
	if exp := os.Getenv("JWT_EXPIRY"); exp != "" {
		if d, err := time.ParseDuration(exp); err == nil {
			expiry = d
		}
	}

	return &Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("POSTGRES_URL", "postgresql://postgres:homeguard-postgres-2024@postgresql.homeguard-data:5432/homeguard?sslmode=disable"),
		JWTSecret:   getEnv("JWT_SECRET", "homeguard-jwt-secret-change-in-production-2024-very-long-key"),
		JWTExpiry:   expiry,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// NewService creates a new user service
func NewService(config *Config) (*Service, error) {
	db, err := sql.Open("postgres", config.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	service := &Service{
		config: config,
		db:     db,
		router: mux.NewRouter(),
	}

	// Initialize schema
	if err := service.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return service, nil
}

func (s *Service) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		email VARCHAR(255) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		name VARCHAR(255) NOT NULL,
		role VARCHAR(50) DEFAULT 'user',
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

	CREATE TABLE IF NOT EXISTS refresh_tokens (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID REFERENCES users(id) ON DELETE CASCADE,
		token VARCHAR(255) UNIQUE NOT NULL,
		expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token ON refresh_tokens(token);
	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SetupRoutes configures all HTTP routes
func (s *Service) SetupRoutes() {
	// Health check
	s.router.HandleFunc("/health", s.healthCheck).Methods("GET")

	// Metrics
	s.router.Handle("/metrics", promhttp.Handler())

	// Auth routes
	s.router.HandleFunc("/auth/login", s.login).Methods("POST")
	s.router.HandleFunc("/auth/register", s.register).Methods("POST")
	s.router.HandleFunc("/auth/refresh", s.refreshToken).Methods("POST")

	// User routes
	s.router.HandleFunc("/users/me", s.getCurrentUser).Methods("GET")
	s.router.HandleFunc("/users/me", s.updateCurrentUser).Methods("PUT")
	s.router.HandleFunc("/users/{id}", s.getUserByID).Methods("GET")
}

func (s *Service) healthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		s.errorResponse(w, http.StatusServiceUnavailable, "Database unhealthy")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" || req.Name == "" {
		s.errorResponse(w, http.StatusBadRequest, "Email, password, and name are required")
		return
	}

	if len(req.Password) < 8 {
		s.errorResponse(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to process registration")
		return
	}

	// Create user
	user := &User{
		ID:           uuid.New().String(),
		Email:        strings.ToLower(req.Email),
		PasswordHash: string(hashedPassword),
		Name:         req.Name,
		Role:         "user",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	query := `INSERT INTO users (id, email, password_hash, name, role, created_at, updated_at)
              VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err = s.db.Exec(query, user.ID, user.Email, user.PasswordHash, user.Name, user.Role, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			s.errorResponse(w, http.StatusConflict, "Email already registered")
			return
		}
		log.Printf("Error creating user: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// Generate tokens
	token, refreshToken, err := s.generateTokens(user)
	if err != nil {
		log.Printf("Error generating tokens: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to generate tokens")
		return
	}

	s.jsonResponse(w, http.StatusCreated, AuthResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.config.JWTExpiry.Seconds()),
		User:         user,
	})
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		s.errorResponse(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	// Find user
	user := &User{}
	query := `SELECT id, email, password_hash, name, role, created_at, updated_at FROM users WHERE email = $1`
	err := s.db.QueryRow(query, strings.ToLower(req.Email)).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt, &user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	if err != nil {
		log.Printf("Error finding user: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to authenticate")
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	// Generate tokens
	token, refreshToken, err := s.generateTokens(user)
	if err != nil {
		log.Printf("Error generating tokens: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to generate tokens")
		return
	}

	s.jsonResponse(w, http.StatusOK, AuthResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.config.JWTExpiry.Seconds()),
		User:         user,
	})
}

func (s *Service) refreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate refresh token
	var userID string
	var expiresAt time.Time
	query := `SELECT user_id, expires_at FROM refresh_tokens WHERE token = $1`
	err := s.db.QueryRow(query, req.RefreshToken).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid refresh token")
		return
	}
	if err != nil {
		log.Printf("Error validating refresh token: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to refresh token")
		return
	}

	if time.Now().After(expiresAt) {
		s.errorResponse(w, http.StatusUnauthorized, "Refresh token expired")
		return
	}

	// Get user
	user := &User{}
	query = `SELECT id, email, password_hash, name, role, created_at, updated_at FROM users WHERE id = $1`
	err = s.db.QueryRow(query, userID).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		log.Printf("Error finding user: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to refresh token")
		return
	}

	// Delete old refresh token
	s.db.Exec("DELETE FROM refresh_tokens WHERE token = $1", req.RefreshToken)

	// Generate new tokens
	token, refreshToken, err := s.generateTokens(user)
	if err != nil {
		log.Printf("Error generating tokens: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to generate tokens")
		return
	}

	s.jsonResponse(w, http.StatusOK, AuthResponse{
		Token:        token,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.config.JWTExpiry.Seconds()),
		User:         user,
	})
}

func (s *Service) generateTokens(user *User) (string, string, error) {
	// Generate access token
	claims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.config.JWTExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "homeguard-api",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		return "", "", err
	}

	// Generate refresh token
	refreshToken := uuid.New().String()
	expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 days

	query := `INSERT INTO refresh_tokens (user_id, token, expires_at) VALUES ($1, $2, $3)`
	_, err = s.db.Exec(query, user.ID, refreshToken, expiresAt)
	if err != nil {
		return "", "", err
	}

	return tokenString, refreshToken, nil
}

func (s *Service) getCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	user, err := s.getUserByIDInternal(userID)
	if err != nil {
		log.Printf("Error getting user: %v", err)
		s.errorResponse(w, http.StatusNotFound, "User not found")
		return
	}

	s.jsonResponse(w, http.StatusOK, user)
}

func (s *Service) updateCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := `UPDATE users SET name = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.db.Exec(query, req.Name, userID)
	if err != nil {
		log.Printf("Error updating user: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	user, _ := s.getUserByIDInternal(userID)
	s.jsonResponse(w, http.StatusOK, user)
}

func (s *Service) getUserByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["id"]

	user, err := s.getUserByIDInternal(userID)
	if err != nil {
		s.errorResponse(w, http.StatusNotFound, "User not found")
		return
	}

	s.jsonResponse(w, http.StatusOK, user)
}

func (s *Service) getUserByIDInternal(userID string) (*User, error) {
	user := &User{}
	query := `SELECT id, email, name, role, created_at, updated_at FROM users WHERE id = $1`
	err := s.db.QueryRow(query, userID).Scan(
		&user.ID, &user.Email, &user.Name, &user.Role, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Service) errorResponse(w http.ResponseWriter, status int, message string) {
	s.jsonResponse(w, status, map[string]interface{}{
		"error":   true,
		"message": message,
		"status":  status,
	})
}

func main() {
	log.Println("Starting HomeGuard User Service...")

	config := loadConfig()
	service, err := NewService(config)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}
	defer service.db.Close()

	service.SetupRoutes()

	server := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      service.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("User Service listening on port %s", config.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
