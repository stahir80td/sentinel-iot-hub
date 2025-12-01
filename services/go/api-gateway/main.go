package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"golang.org/x/time/rate"
)

// Config holds the application configuration
type Config struct {
	Port                   string
	JWTSecret              string
	UserServiceURL         string
	DeviceServiceURL       string
	DeviceIngestURL        string
	NotificationServiceURL string
	AnalyticsServiceURL    string
	AgenticAIURL           string
	ScenarioEngineURL      string
	RateLimitPerMinute     int
	RateLimitBurst         int
}

// Claims represents JWT claims
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// RateLimiter manages per-client rate limiting
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	r        rate.Limit
	b        int
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// Metrics for Prometheus
var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_gateway_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "api_gateway_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
	activeConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "api_gateway_active_connections",
			Help: "Number of active connections",
		},
	)
	wsActivityConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "api_gateway_ws_activity_connections",
			Help: "Number of active WebSocket activity stream connections",
		},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpRequestDuration)
	prometheus.MustRegister(activeConnections)
	prometheus.MustRegister(wsActivityConnections)
}

func loadConfig() *Config {
	return &Config{
		Port:                   getEnv("PORT", "8080"),
		JWTSecret:              getEnv("JWT_SECRET", "homeguard-jwt-secret-change-in-production-2024-very-long-key"),
		UserServiceURL:         getEnv("USER_SERVICE_URL", "http://user-service:8080"),
		DeviceServiceURL:       getEnv("DEVICE_SERVICE_URL", "http://device-service:8080"),
		DeviceIngestURL:        getEnv("DEVICE_INGEST_URL", "http://device-ingest:8080"),
		NotificationServiceURL: getEnv("NOTIFICATION_SERVICE_URL", "http://notification-service:8080"),
		AnalyticsServiceURL:    getEnv("ANALYTICS_SERVICE_URL", "http://analytics-service:8080"),
		AgenticAIURL:           getEnv("AGENTIC_AI_URL", "http://agentic-ai:8080"),
		ScenarioEngineURL:      getEnv("SCENARIO_ENGINE_URL", "http://scenario-engine:8080"),
		RateLimitPerMinute:     getEnvInt("RATE_LIMIT_REQUESTS_PER_MINUTE", 100),
		RateLimitBurst:         getEnvInt("RATE_LIMIT_BURST", 20),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		fmt.Sscanf(value, "%d", &result)
		if result > 0 {
			return result
		}
	}
	return defaultValue
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        r,
		b:        b,
	}
}

// GetLimiter returns a rate limiter for a given client
func (rl *RateLimiter) GetLimiter(clientID string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[clientID]
	if !exists {
		limiter = rate.NewLimiter(rl.r, rl.b)
		rl.limiters[clientID] = limiter
	}
	return limiter
}

// Gateway is the main API gateway struct
type Gateway struct {
	config      *Config
	router      *mux.Router
	rateLimiter *RateLimiter
}

// NewGateway creates a new API gateway
func NewGateway(config *Config) *Gateway {
	rateLimit := rate.Limit(float64(config.RateLimitPerMinute) / 60.0)
	return &Gateway{
		config:      config,
		router:      mux.NewRouter(),
		rateLimiter: NewRateLimiter(rateLimit, config.RateLimitBurst),
	}
}

// SetupRoutes configures all API routes
func (g *Gateway) SetupRoutes() {
	// Health check
	g.router.HandleFunc("/health", g.healthCheck).Methods("GET")
	g.router.HandleFunc("/ready", g.readinessCheck).Methods("GET")

	// Metrics
	g.router.Handle("/metrics", promhttp.Handler())

	// Public routes (no auth) - support both /api and /api/v1 prefixes
	g.router.HandleFunc("/api/auth/login", g.proxyHandler(g.config.UserServiceURL)).Methods("POST")
	g.router.HandleFunc("/api/auth/register", g.proxyHandler(g.config.UserServiceURL)).Methods("POST")
	g.router.HandleFunc("/api/auth/refresh", g.proxyHandler(g.config.UserServiceURL)).Methods("POST")
	g.router.HandleFunc("/api/v1/auth/login", g.proxyHandler(g.config.UserServiceURL)).Methods("POST")
	g.router.HandleFunc("/api/v1/auth/register", g.proxyHandler(g.config.UserServiceURL)).Methods("POST")
	g.router.HandleFunc("/api/v1/auth/refresh", g.proxyHandler(g.config.UserServiceURL)).Methods("POST")

	// Device ingestion (device-token auth, not user JWT)
	g.router.HandleFunc("/api/v1/ingest/{path:.*}", g.deviceAuthMiddleware(g.proxyHandler(g.config.DeviceIngestURL))).Methods("POST")

	// WebSocket routes - registered directly on main router to avoid middleware wrapping ResponseWriter
	// These handlers do their own auth via Sec-WebSocket-Protocol header
	g.router.HandleFunc("/api/activity/stream", g.activityStreamHandler).Methods("GET")
	g.router.HandleFunc("/api/v1/activity/stream", g.activityStreamHandler).Methods("GET")

	// Protected routes (require JWT) - support both /api and /api/v1 prefixes
	apiV1 := g.router.PathPrefix("/api/v1").Subrouter()
	apiV1.Use(g.authMiddleware)
	apiV1.Use(g.rateLimitMiddleware)

	api := g.router.PathPrefix("/api").Subrouter()
	api.Use(g.authMiddleware)
	api.Use(g.rateLimitMiddleware)

	// Register routes for both prefixes
	for _, r := range []*mux.Router{api, apiV1} {
		// User routes
		r.HandleFunc("/users/me", g.proxyHandler(g.config.UserServiceURL)).Methods("GET", "PUT")
		r.HandleFunc("/users/{id}", g.proxyHandler(g.config.UserServiceURL)).Methods("GET")

		// Device routes
		r.HandleFunc("/devices", g.proxyHandler(g.config.DeviceServiceURL)).Methods("GET", "POST")
		r.HandleFunc("/devices/{id}", g.proxyHandler(g.config.DeviceServiceURL)).Methods("GET", "PUT", "PATCH", "DELETE")
		r.HandleFunc("/devices/{id}/command", g.proxyHandler(g.config.DeviceServiceURL)).Methods("POST")
		r.HandleFunc("/devices/{id}/status", g.proxyHandler(g.config.DeviceServiceURL)).Methods("GET")
		r.HandleFunc("/devices/{id}/events", g.proxyHandler(g.config.DeviceServiceURL)).Methods("GET")

		// Notification routes
		r.HandleFunc("/notifications", g.proxyHandler(g.config.NotificationServiceURL)).Methods("GET")
		r.HandleFunc("/notifications/{id}/read", g.proxyHandler(g.config.NotificationServiceURL)).Methods("PUT")
		r.HandleFunc("/notifications/preferences", g.proxyHandler(g.config.NotificationServiceURL)).Methods("GET", "PUT")

		// Analytics routes
		r.HandleFunc("/analytics/summary", g.proxyHandler(g.config.AnalyticsServiceURL)).Methods("GET")
		r.HandleFunc("/analytics/devices/{id}", g.proxyHandler(g.config.AnalyticsServiceURL)).Methods("GET")
		r.HandleFunc("/analytics/trends", g.proxyHandler(g.config.AnalyticsServiceURL)).Methods("GET")

		// AI Agent routes
		r.HandleFunc("/agent/chat", g.proxyHandler(g.config.AgenticAIURL)).Methods("POST")
		r.HandleFunc("/agent/stream", g.proxyHandler(g.config.AgenticAIURL)).Methods("POST")
		r.HandleFunc("/agent/history", g.proxyHandler(g.config.AgenticAIURL)).Methods("GET", "DELETE")
		r.HandleFunc("/agent/suggestions", g.proxyHandler(g.config.AgenticAIURL)).Methods("GET")

		// Scenario/Automation routes
		r.HandleFunc("/scenarios", g.proxyHandler(g.config.ScenarioEngineURL)).Methods("GET", "POST")
		r.HandleFunc("/scenarios/{id}", g.proxyHandler(g.config.ScenarioEngineURL)).Methods("GET", "PUT", "DELETE")
		r.HandleFunc("/scenarios/{id}/enable", g.proxyHandler(g.config.ScenarioEngineURL)).Methods("POST")
		r.HandleFunc("/scenarios/{id}/disable", g.proxyHandler(g.config.ScenarioEngineURL)).Methods("POST")

		// Activity stream routes (non-WebSocket)
		r.HandleFunc("/activity/recent", g.proxyHandler(g.config.NotificationServiceURL)).Methods("GET")

		// General WebSocket endpoint
		r.HandleFunc("/ws", g.websocketHandler).Methods("GET")
		// Note: /activity/stream is registered directly on main router to avoid middleware ResponseWriter wrapping
	}
}

func (g *Gateway) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (g *Gateway) readinessCheck(w http.ResponseWriter, r *http.Request) {
	// Check downstream services
	services := map[string]string{
		"user-service":   g.config.UserServiceURL,
		"device-service": g.config.DeviceServiceURL,
	}

	allReady := true
	status := make(map[string]string)

	for name, url := range services {
		resp, err := http.Get(url + "/health")
		if err != nil || resp.StatusCode != 200 {
			status[name] = "unhealthy"
			allReady = false
		} else {
			status[name] = "healthy"
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if !allReady {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ready":    allReady,
		"services": status,
	})
}

func (g *Gateway) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tokenString string

		// First, try Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				g.errorResponse(w, http.StatusUnauthorized, "Invalid authorization format")
				return
			}
		}

		// For WebSocket connections, also check Sec-WebSocket-Protocol header
		// Browser WebSocket API uses subprotocol for auth: new WebSocket(url, ['Bearer', token])
		if tokenString == "" {
			wsProtocol := r.Header.Get("Sec-WebSocket-Protocol")
			if wsProtocol != "" {
				// Format: "Bearer, <token>" - browser joins subprotocols with ", "
				parts := strings.Split(wsProtocol, ", ")
				if len(parts) == 2 && parts[0] == "Bearer" {
					tokenString = parts[1]
				}
			}
		}

		if tokenString == "" {
			g.errorResponse(w, http.StatusUnauthorized, "Missing authorization header")
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(g.config.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			g.errorResponse(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		// Add user info to request context
		ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
		ctx = context.WithValue(ctx, "user_email", claims.Email)
		ctx = context.WithValue(ctx, "user_role", claims.Role)

		// Add user info to headers for downstream services
		r.Header.Set("X-User-ID", claims.UserID)
		r.Header.Set("X-User-Email", claims.Email)
		r.Header.Set("X-User-Role", claims.Role)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (g *Gateway) deviceAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceToken := r.Header.Get("X-Device-Token")
		if deviceToken == "" {
			g.errorResponse(w, http.StatusUnauthorized, "Missing device token")
			return
		}
		// Device token validation is done by the device-ingest service
		next.ServeHTTP(w, r)
	}
}

func (g *Gateway) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := r.Header.Get("X-User-ID")
		if clientID == "" {
			clientID = r.RemoteAddr
		}

		limiter := g.rateLimiter.GetLimiter(clientID)
		if !limiter.Allow() {
			g.errorResponse(w, http.StatusTooManyRequests, "Rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) proxyHandler(targetURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target, err := url.Parse(targetURL)
		if err != nil {
			g.errorResponse(w, http.StatusInternalServerError, "Invalid target URL")
			return
		}

		proxy := httputil.NewSingleHostReverseProxy(target)

		// Use a longer timeout transport for all proxied requests
		proxy.Transport = &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 120 * time.Second, // Wait up to 2 min for AI response
		}

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("Proxy error: %v", err)
			g.errorResponse(w, http.StatusBadGateway, "Service unavailable")
		}

		// Modify the request
		proxy.Director = func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			// Strip /api or /api/v1 prefix for downstream services
			path := r.URL.Path
			if strings.HasPrefix(path, "/api/v1/") {
				req.URL.Path = strings.TrimPrefix(path, "/api/v1")
			} else if strings.HasPrefix(path, "/api/") {
				req.URL.Path = strings.TrimPrefix(path, "/api")
			}
		}

		proxy.ServeHTTP(w, r)
	}
}

func (g *Gateway) websocketHandler(w http.ResponseWriter, r *http.Request) {
	// General WebSocket endpoint for future use
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Keep connection alive with pings
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// activityStreamHandler proxies WebSocket connections to the notification service activity stream
// This handler does its own auth because it's registered directly on the main router to avoid ResponseWriter wrapping
func (g *Gateway) activityStreamHandler(w http.ResponseWriter, r *http.Request) {
	// Authenticate via Sec-WebSocket-Protocol header (browser WebSocket sends token as subprotocol)
	wsProtocol := r.Header.Get("Sec-WebSocket-Protocol")
	if wsProtocol == "" {
		g.errorResponse(w, http.StatusUnauthorized, "Missing authentication")
		return
	}

	// Parse "Bearer, <token>" format
	var tokenString string
	parts := strings.Split(wsProtocol, ", ")
	if len(parts) == 2 && parts[0] == "Bearer" {
		tokenString = parts[1]
	}
	if tokenString == "" {
		g.errorResponse(w, http.StatusUnauthorized, "Invalid authentication format")
		return
	}

	// Validate JWT token
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(g.config.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		g.errorResponse(w, http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	userID := claims.UserID

	// Parse the notification service URL
	targetURL, err := url.Parse(g.config.NotificationServiceURL)
	if err != nil {
		log.Printf("Error parsing notification service URL: %v", err)
		g.errorResponse(w, http.StatusInternalServerError, "Configuration error")
		return
	}

	// Build the WebSocket URL for the notification service
	wsScheme := "ws"
	if targetURL.Scheme == "https" {
		wsScheme = "wss"
	}
	backendURL := fmt.Sprintf("%s://%s/activity/stream/%s", wsScheme, targetURL.Host, userID)

	log.Printf("[ACTIVITY] Proxying WebSocket for user %s to %s", userID, backendURL)

	// IMPORTANT: Upgrade client connection FIRST before any other operations
	// The http.ResponseWriter must not be used before upgrading, otherwise Hijacker fails
	// For subprotocol auth, browser sends "Bearer, <token>" but we must respond with just "Bearer"
	var responseHeader http.Header
	if wsProtocol := r.Header.Get("Sec-WebSocket-Protocol"); wsProtocol != "" {
		responseHeader = http.Header{}
		// Only respond with the protocol name, not the token
		parts := strings.Split(wsProtocol, ", ")
		if len(parts) >= 1 {
			responseHeader.Set("Sec-WebSocket-Protocol", parts[0])
		}
	}
	clientConn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Printf("Client WebSocket upgrade error: %v", err)
		return
	}
	defer clientConn.Close()

	wsActivityConnections.Inc()
	defer wsActivityConnections.Dec()

	// Now connect to backend WebSocket
	backendConn, resp, err := websocket.DefaultDialer.Dial(backendURL, nil)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Backend WebSocket connection failed: %v, status: %d, body: %s", err, resp.StatusCode, string(body))
		} else {
			log.Printf("Backend WebSocket connection failed: %v", err)
		}
		clientConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Failed to connect to activity stream"))
		return
	}
	defer backendConn.Close()

	log.Printf("[ACTIVITY] WebSocket connection established for user %s", userID)

	// Proxy messages between client and backend
	done := make(chan struct{})

	// Backend to client
	go func() {
		defer close(done)
		for {
			messageType, message, err := backendConn.ReadMessage()
			if err != nil {
				log.Printf("[ACTIVITY] Backend read error: %v", err)
				return
			}
			if err := clientConn.WriteMessage(messageType, message); err != nil {
				log.Printf("[ACTIVITY] Client write error: %v", err)
				return
			}
		}
	}()

	// Client to backend (for pings/pongs)
	go func() {
		for {
			messageType, message, err := clientConn.ReadMessage()
			if err != nil {
				log.Printf("[ACTIVITY] Client read error: %v", err)
				backendConn.Close()
				return
			}
			if err := backendConn.WriteMessage(messageType, message); err != nil {
				log.Printf("[ACTIVITY] Backend write error: %v", err)
				return
			}
		}
	}()

	<-done
	log.Printf("[ACTIVITY] WebSocket connection closed for user %s", userID)
}

func (g *Gateway) errorResponse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   true,
		"message": message,
		"status":  status,
	})
}

// metricsMiddleware records metrics for all requests
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		activeConnections.Inc()
		defer activeConnections.Dec()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		path := r.URL.Path
		method := r.Method

		httpRequestsTotal.WithLabelValues(method, path, fmt.Sprintf("%d", wrapped.statusCode)).Inc()
		httpRequestDuration.WithLabelValues(method, path).Observe(duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker interface for WebSocket support
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

func main() {
	log.Println("Starting HomeGuard API Gateway...")

	config := loadConfig()
	gateway := NewGateway(config)
	gateway.SetupRoutes()

	// Setup CORS
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://homeguard.localhost", "*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Device-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	// Wrap router with CORS and metrics
	handler := c.Handler(metricsMiddleware(gateway.router))

	server := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // Increased for AI service which can take longer with retries
		IdleTimeout:  120 * time.Second,
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

	log.Printf("API Gateway listening on port %s", config.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
