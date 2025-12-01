package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config holds the application configuration
type Config struct {
	Port     string
	RedisURL string
}

// Notification represents a notification to be sent
type Notification struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	DeviceID  string                 `json:"device_id,omitempty"`
	Type      string                 `json:"type"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Priority  string                 `json:"priority"`
	Timestamp time.Time              `json:"timestamp"`
	Read      bool                   `json:"read"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

// ActivityEvent represents a system activity event for the activity panel
type ActivityEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`    // ai, device, kafka, redis, mongodb, timescaledb, n8n, processor
	Icon      string    `json:"icon"`      // emoji icon
	Action    string    `json:"action"`    // short action description
	Details   string    `json:"details"`   // detailed message
	UserID    string    `json:"user_id"`
	DeviceID  string    `json:"device_id,omitempty"`
	Severity  string    `json:"severity"`  // info, warning, alert
}

// Metrics
var (
	notificationsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_service_sent_total",
			Help: "Total notifications sent",
		},
		[]string{"type", "priority"},
	)
	activeConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "notification_service_active_connections",
			Help: "Number of active WebSocket connections",
		},
	)
	activityConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "notification_service_activity_connections",
			Help: "Number of active activity WebSocket connections",
		},
	)
	notificationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_service_errors_total",
			Help: "Total notification errors",
		},
		[]string{"type"},
	)
	activityEventsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_service_activity_events_total",
			Help: "Total activity events received",
		},
		[]string{"source"},
	)
	activityEventsBroadcast = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "notification_service_activity_broadcast_total",
			Help: "Total activity events broadcast to clients",
		},
	)
)

func init() {
	prometheus.MustRegister(notificationsSent)
	prometheus.MustRegister(activeConnections)
	prometheus.MustRegister(activityConnections)
	prometheus.MustRegister(notificationErrors)
	prometheus.MustRegister(activityEventsReceived)
	prometheus.MustRegister(activityEventsBroadcast)
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// Client represents a WebSocket client
type Client struct {
	conn   *websocket.Conn
	userID string
	send   chan []byte
}

// ActivityClient represents a WebSocket client for activity stream
type ActivityClient struct {
	conn   *websocket.Conn
	userID string
	send   chan []byte
}

// Service handles notifications
type Service struct {
	config          *Config
	redis           *redis.Client
	router          *mux.Router
	clients         map[string]*Client
	activityClients map[string]*ActivityClient
	mu              sync.RWMutex
	activityMu      sync.RWMutex
}

func loadConfig() *Config {
	return &Config{
		Port:     getEnv("PORT", "8080"),
		RedisURL: getEnv("REDIS_URL", "redis://redis.homeguard-data:6379"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// NewService creates a new notification service
func NewService(config *Config) (*Service, error) {
	opt, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	redisClient := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Redis connection failed: %v", err)
	}

	return &Service{
		config:          config,
		redis:           redisClient,
		router:          mux.NewRouter(),
		clients:         make(map[string]*Client),
		activityClients: make(map[string]*ActivityClient),
	}, nil
}

// SetupRoutes configures HTTP routes
func (s *Service) SetupRoutes() {
	s.router.HandleFunc("/health", s.healthCheck).Methods("GET")
	s.router.Handle("/metrics", promhttp.Handler())

	// Notification endpoints
	s.router.HandleFunc("/notify", s.sendNotification).Methods("POST")
	s.router.HandleFunc("/notifications/{user_id}", s.getUserNotifications).Methods("GET")
	s.router.HandleFunc("/notifications/{user_id}/{notification_id}/read", s.markAsRead).Methods("POST")
	s.router.HandleFunc("/notifications/{user_id}/read-all", s.markAllAsRead).Methods("POST")

	// WebSocket endpoint for notifications
	s.router.HandleFunc("/ws/{user_id}", s.handleWebSocket)

	// Activity stream endpoints
	s.router.HandleFunc("/activity", s.postActivity).Methods("POST")
	s.router.HandleFunc("/activity/stream/{user_id}", s.handleActivityWebSocket)
	s.router.HandleFunc("/activity/recent/{user_id}", s.getRecentActivity).Methods("GET")
}

func (s *Service) healthCheck(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Service) sendNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   string                 `json:"user_id"`
		DeviceID string                 `json:"device_id,omitempty"`
		Type     string                 `json:"type"`
		Title    string                 `json:"title"`
		Message  string                 `json:"message"`
		Priority string                 `json:"priority"`
		Payload  map[string]interface{} `json:"payload,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.UserID == "" {
		s.errorResponse(w, http.StatusBadRequest, "user_id is required")
		return
	}

	if req.Priority == "" {
		req.Priority = "normal"
	}

	if req.Type == "" {
		req.Type = "general"
	}

	notification := Notification{
		ID:        uuid.New().String(),
		UserID:    req.UserID,
		DeviceID:  req.DeviceID,
		Type:      req.Type,
		Title:     req.Title,
		Message:   req.Message,
		Priority:  req.Priority,
		Timestamp: time.Now(),
		Read:      false,
		Payload:   req.Payload,
	}

	// Store notification in Redis
	ctx := context.Background()
	notificationJSON, _ := json.Marshal(notification)
	key := fmt.Sprintf("notifications:%s", req.UserID)

	if err := s.redis.LPush(ctx, key, notificationJSON).Err(); err != nil {
		log.Printf("Failed to store notification: %v", err)
		notificationErrors.WithLabelValues("storage").Inc()
	}

	// Trim to keep only last 100 notifications
	s.redis.LTrim(ctx, key, 0, 99)

	// Send via WebSocket if client is connected
	s.sendToClient(req.UserID, notification)

	notificationsSent.WithLabelValues(req.Type, req.Priority).Inc()

	s.jsonResponse(w, http.StatusAccepted, map[string]interface{}{
		"id":      notification.ID,
		"status":  "sent",
		"message": "Notification queued",
	})
}

func (s *Service) getUserNotifications(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	ctx := context.Background()
	key := fmt.Sprintf("notifications:%s", userID)

	notifications, err := s.redis.LRange(ctx, key, 0, 49).Result()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to retrieve notifications")
		return
	}

	result := make([]Notification, 0, len(notifications))
	for _, n := range notifications {
		var notification Notification
		if err := json.Unmarshal([]byte(n), &notification); err == nil {
			result = append(result, notification)
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"notifications": result,
		"count":         len(result),
	})
}

func (s *Service) markAsRead(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	notificationID := vars["notification_id"]

	ctx := context.Background()
	key := fmt.Sprintf("notifications:%s", userID)

	// Get all notifications
	notifications, err := s.redis.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to retrieve notifications")
		return
	}

	// Find and update the notification
	for i, n := range notifications {
		var notification Notification
		if err := json.Unmarshal([]byte(n), &notification); err == nil {
			if notification.ID == notificationID {
				notification.Read = true
				updated, _ := json.Marshal(notification)
				s.redis.LSet(ctx, key, int64(i), string(updated))
				break
			}
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "marked as read"})
}

func (s *Service) markAllAsRead(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	ctx := context.Background()
	key := fmt.Sprintf("notifications:%s", userID)

	notifications, err := s.redis.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to retrieve notifications")
		return
	}

	for i, n := range notifications {
		var notification Notification
		if err := json.Unmarshal([]byte(n), &notification); err == nil {
			notification.Read = true
			updated, _ := json.Marshal(notification)
			s.redis.LSet(ctx, key, int64(i), string(updated))
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "all marked as read"})
}

func (s *Service) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		conn:   conn,
		userID: userID,
		send:   make(chan []byte, 256),
	}

	s.mu.Lock()
	s.clients[userID] = client
	s.mu.Unlock()

	activeConnections.Inc()

	go s.writePump(client)
	s.readPump(client)
}

func (s *Service) readPump(client *Client) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, client.userID)
		s.mu.Unlock()
		client.conn.Close()
		activeConnections.Dec()
	}()

	client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
	}
}

func (s *Service) writePump(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *Service) sendToClient(userID string, notification Notification) {
	s.mu.RLock()
	client, exists := s.clients[userID]
	s.mu.RUnlock()

	if exists {
		message, _ := json.Marshal(map[string]interface{}{
			"type":         "notification",
			"notification": notification,
		})

		select {
		case client.send <- message:
		default:
			log.Printf("Client %s send buffer full", userID)
		}
	}
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

// ============================================
// Activity Stream Handlers
// ============================================

// postActivity receives activity events from other services and broadcasts to connected clients
func (s *Service) postActivity(w http.ResponseWriter, r *http.Request) {
	var event ActivityEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Generate ID and timestamp if not provided
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Severity == "" {
		event.Severity = "info"
	}

	// Log for debugging and Grafana/Loki
	log.Printf("[ACTIVITY] source=%s action=%s details=%s user=%s device=%s severity=%s",
		event.Source, event.Action, event.Details, event.UserID, event.DeviceID, event.Severity)

	// Store in Redis for recent activity history
	ctx := context.Background()
	activityJSON, _ := json.Marshal(event)

	// Store globally and per-user
	s.redis.LPush(ctx, "activity:global", activityJSON)
	s.redis.LTrim(ctx, "activity:global", 0, 99) // Keep last 100

	if event.UserID != "" {
		userKey := fmt.Sprintf("activity:%s", event.UserID)
		s.redis.LPush(ctx, userKey, activityJSON)
		s.redis.LTrim(ctx, userKey, 0, 99)
	}

	// Broadcast to connected activity clients
	s.broadcastActivity(event)

	activityEventsReceived.WithLabelValues(event.Source).Inc()

	s.jsonResponse(w, http.StatusAccepted, map[string]interface{}{
		"id":     event.ID,
		"status": "broadcast",
	})
}

// broadcastActivity sends activity event to all connected clients (or specific user)
func (s *Service) broadcastActivity(event ActivityEvent) {
	s.activityMu.RLock()
	defer s.activityMu.RUnlock()

	// Send the event directly, not wrapped
	message, _ := json.Marshal(event)

	// Broadcast to matching user or all if no user specified
	for userID, client := range s.activityClients {
		if event.UserID == "" || event.UserID == userID {
			select {
			case client.send <- message:
				activityEventsBroadcast.Inc()
			default:
				log.Printf("Activity client %s send buffer full", userID)
			}
		}
	}
}

// handleActivityWebSocket handles WebSocket connections for activity stream
func (s *Service) handleActivityWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Activity WebSocket upgrade error: %v", err)
		return
	}

	client := &ActivityClient{
		conn:   conn,
		userID: userID,
		send:   make(chan []byte, 256),
	}

	s.activityMu.Lock()
	s.activityClients[userID] = client
	s.activityMu.Unlock()

	activityConnections.Inc()
	log.Printf("Activity client connected: %s", userID)

	go s.activityWritePump(client)
	s.activityReadPump(client)
}

func (s *Service) activityReadPump(client *ActivityClient) {
	defer func() {
		s.activityMu.Lock()
		delete(s.activityClients, client.userID)
		s.activityMu.Unlock()
		client.conn.Close()
		activityConnections.Dec()
		log.Printf("Activity client disconnected: %s", client.userID)
	}()

	client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Activity WebSocket error: %v", err)
			}
			break
		}
	}
}

func (s *Service) activityWritePump(client *ActivityClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// getRecentActivity returns recent activity events from Redis
func (s *Service) getRecentActivity(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	ctx := context.Background()
	key := fmt.Sprintf("activity:%s", userID)

	activities, err := s.redis.LRange(ctx, key, 0, 49).Result()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to retrieve activities")
		return
	}

	result := make([]ActivityEvent, 0, len(activities))
	for _, a := range activities {
		var activity ActivityEvent
		if err := json.Unmarshal([]byte(a), &activity); err == nil {
			result = append(result, activity)
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"activities": result,
		"count":      len(result),
	})
}

func main() {
	log.Println("Starting HomeGuard Notification Service...")

	config := loadConfig()
	service, err := NewService(config)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}
	defer service.redis.Close()

	service.SetupRoutes()

	server := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      service.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

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

	log.Printf("Notification Service listening on port %s", config.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
