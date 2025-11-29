package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config holds the application configuration
type Config struct {
	Port              string
	RedisURL          string
	DeviceServiceURL  string
	NotificationURL   string
}

// Scenario represents an automation scenario
type Scenario struct {
	ID          string      `json:"id"`
	UserID      string      `json:"user_id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Enabled     bool        `json:"enabled"`
	Trigger     Trigger     `json:"trigger"`
	Conditions  []Condition `json:"conditions,omitempty"`
	Actions     []Action    `json:"actions"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// Trigger defines what starts the scenario
type Trigger struct {
	Type     string                 `json:"type"` // device_event, schedule, manual
	DeviceID string                 `json:"device_id,omitempty"`
	Event    string                 `json:"event,omitempty"`
	Schedule string                 `json:"schedule,omitempty"` // cron expression
	Params   map[string]interface{} `json:"params,omitempty"`
}

// Condition defines when actions should execute
type Condition struct {
	Type     string      `json:"type"` // device_state, time_range, value_compare
	DeviceID string      `json:"device_id,omitempty"`
	Property string      `json:"property,omitempty"`
	Operator string      `json:"operator"` // eq, ne, gt, lt, gte, lte, contains
	Value    interface{} `json:"value"`
}

// Action defines what happens when triggered
type Action struct {
	Type     string                 `json:"type"` // device_command, notification, webhook, delay
	DeviceID string                 `json:"device_id,omitempty"`
	Command  string                 `json:"command,omitempty"`
	Params   map[string]interface{} `json:"params,omitempty"`
	Delay    int                    `json:"delay,omitempty"` // seconds
}

// EventPayload represents an incoming event
type EventPayload struct {
	EventID   string                 `json:"event_id"`
	DeviceID  string                 `json:"device_id"`
	UserID    string                 `json:"user_id"`
	EventType string                 `json:"event_type"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// Metrics
var (
	scenariosEvaluated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "scenario_engine_evaluations_total",
			Help: "Total scenario evaluations",
		},
	)
	scenariosTriggered = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scenario_engine_triggered_total",
			Help: "Total scenarios triggered",
		},
		[]string{"scenario_id"},
	)
	actionsExecuted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scenario_engine_actions_executed_total",
			Help: "Total actions executed",
		},
		[]string{"action_type"},
	)
	evaluationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "scenario_engine_evaluation_seconds",
			Help:    "Time spent evaluating scenarios",
			Buckets: prometheus.DefBuckets,
		},
	)
)

func init() {
	prometheus.MustRegister(scenariosEvaluated)
	prometheus.MustRegister(scenariosTriggered)
	prometheus.MustRegister(actionsExecuted)
	prometheus.MustRegister(evaluationDuration)
}

// Service handles scenario automation
type Service struct {
	config    *Config
	redis     *redis.Client
	router    *mux.Router
	client    *http.Client
	scenarios map[string][]Scenario // userID -> scenarios
	mu        sync.RWMutex
}

func loadConfig() *Config {
	return &Config{
		Port:             getEnv("PORT", "8080"),
		RedisURL:         getEnv("REDIS_URL", "redis://redis.homeguard-data:6379"),
		DeviceServiceURL: getEnv("DEVICE_SERVICE_URL", "http://device-service:8080"),
		NotificationURL:  getEnv("NOTIFICATION_SERVICE_URL", "http://notification-service:8080"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// NewService creates a new scenario engine service
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

	service := &Service{
		config:    config,
		redis:     redisClient,
		router:    mux.NewRouter(),
		client:    &http.Client{Timeout: 10 * time.Second},
		scenarios: make(map[string][]Scenario),
	}

	// Load scenarios from Redis
	service.loadScenarios()

	return service, nil
}

func (s *Service) loadScenarios() {
	ctx := context.Background()
	keys, err := s.redis.Keys(ctx, "scenarios:*").Result()
	if err != nil {
		log.Printf("Failed to load scenario keys: %v", err)
		return
	}

	for _, key := range keys {
		scenarios, err := s.redis.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			continue
		}

		userID := strings.TrimPrefix(key, "scenarios:")
		for _, scenarioJSON := range scenarios {
			var scenario Scenario
			if err := json.Unmarshal([]byte(scenarioJSON), &scenario); err == nil {
				s.scenarios[userID] = append(s.scenarios[userID], scenario)
			}
		}
	}
	log.Printf("Loaded %d scenario groups", len(s.scenarios))
}

// SetupRoutes configures HTTP routes
func (s *Service) SetupRoutes() {
	s.router.HandleFunc("/health", s.healthCheck).Methods("GET")
	s.router.Handle("/metrics", promhttp.Handler())

	// Scenario management
	s.router.HandleFunc("/scenarios", s.createScenario).Methods("POST")
	s.router.HandleFunc("/scenarios/{user_id}", s.getUserScenarios).Methods("GET")
	s.router.HandleFunc("/scenarios/{user_id}/{scenario_id}", s.getScenario).Methods("GET")
	s.router.HandleFunc("/scenarios/{user_id}/{scenario_id}", s.updateScenario).Methods("PUT")
	s.router.HandleFunc("/scenarios/{user_id}/{scenario_id}", s.deleteScenario).Methods("DELETE")
	s.router.HandleFunc("/scenarios/{user_id}/{scenario_id}/enable", s.enableScenario).Methods("POST")
	s.router.HandleFunc("/scenarios/{user_id}/{scenario_id}/disable", s.disableScenario).Methods("POST")
	s.router.HandleFunc("/scenarios/{user_id}/{scenario_id}/trigger", s.manualTrigger).Methods("POST")

	// Event evaluation endpoint
	s.router.HandleFunc("/evaluate", s.evaluateEvent).Methods("POST")
}

func (s *Service) healthCheck(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Service) createScenario(w http.ResponseWriter, r *http.Request) {
	var req Scenario
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.UserID == "" || req.Name == "" {
		s.errorResponse(w, http.StatusBadRequest, "user_id and name are required")
		return
	}

	if len(req.Actions) == 0 {
		s.errorResponse(w, http.StatusBadRequest, "At least one action is required")
		return
	}

	req.ID = uuid.New().String()
	req.CreatedAt = time.Now()
	req.UpdatedAt = time.Now()
	req.Enabled = true

	// Store in Redis
	ctx := context.Background()
	key := fmt.Sprintf("scenarios:%s", req.UserID)
	scenarioJSON, _ := json.Marshal(req)

	if err := s.redis.LPush(ctx, key, scenarioJSON).Err(); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to create scenario")
		return
	}

	// Update in-memory cache
	s.mu.Lock()
	s.scenarios[req.UserID] = append(s.scenarios[req.UserID], req)
	s.mu.Unlock()

	s.jsonResponse(w, http.StatusCreated, req)
}

func (s *Service) getUserScenarios(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	s.mu.RLock()
	scenarios := s.scenarios[userID]
	s.mu.RUnlock()

	if scenarios == nil {
		scenarios = []Scenario{}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"scenarios": scenarios,
		"count":     len(scenarios),
	})
}

func (s *Service) getScenario(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	scenarioID := vars["scenario_id"]

	s.mu.RLock()
	scenarios := s.scenarios[userID]
	s.mu.RUnlock()

	for _, scenario := range scenarios {
		if scenario.ID == scenarioID {
			s.jsonResponse(w, http.StatusOK, scenario)
			return
		}
	}

	s.errorResponse(w, http.StatusNotFound, "Scenario not found")
}

func (s *Service) updateScenario(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	scenarioID := vars["scenario_id"]

	var req Scenario
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	scenarios := s.scenarios[userID]
	for i, scenario := range scenarios {
		if scenario.ID == scenarioID {
			req.ID = scenarioID
			req.UserID = userID
			req.CreatedAt = scenario.CreatedAt
			req.UpdatedAt = time.Now()

			scenarios[i] = req
			s.scenarios[userID] = scenarios

			// Update in Redis
			s.persistScenarios(userID)

			s.jsonResponse(w, http.StatusOK, req)
			return
		}
	}

	s.errorResponse(w, http.StatusNotFound, "Scenario not found")
}

func (s *Service) deleteScenario(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	scenarioID := vars["scenario_id"]

	s.mu.Lock()
	defer s.mu.Unlock()

	scenarios := s.scenarios[userID]
	for i, scenario := range scenarios {
		if scenario.ID == scenarioID {
			s.scenarios[userID] = append(scenarios[:i], scenarios[i+1:]...)
			s.persistScenarios(userID)
			s.jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
	}

	s.errorResponse(w, http.StatusNotFound, "Scenario not found")
}

func (s *Service) enableScenario(w http.ResponseWriter, r *http.Request) {
	s.setScenarioEnabled(w, r, true)
}

func (s *Service) disableScenario(w http.ResponseWriter, r *http.Request) {
	s.setScenarioEnabled(w, r, false)
}

func (s *Service) setScenarioEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	scenarioID := vars["scenario_id"]

	s.mu.Lock()
	defer s.mu.Unlock()

	scenarios := s.scenarios[userID]
	for i, scenario := range scenarios {
		if scenario.ID == scenarioID {
			scenarios[i].Enabled = enabled
			scenarios[i].UpdatedAt = time.Now()
			s.scenarios[userID] = scenarios
			s.persistScenarios(userID)
			s.jsonResponse(w, http.StatusOK, scenarios[i])
			return
		}
	}

	s.errorResponse(w, http.StatusNotFound, "Scenario not found")
}

func (s *Service) manualTrigger(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	scenarioID := vars["scenario_id"]

	s.mu.RLock()
	scenarios := s.scenarios[userID]
	s.mu.RUnlock()

	for _, scenario := range scenarios {
		if scenario.ID == scenarioID {
			go s.executeScenario(scenario, nil)
			s.jsonResponse(w, http.StatusAccepted, map[string]string{
				"status":  "triggered",
				"message": "Scenario execution started",
			})
			return
		}
	}

	s.errorResponse(w, http.StatusNotFound, "Scenario not found")
}

func (s *Service) evaluateEvent(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		evaluationDuration.Observe(time.Since(start).Seconds())
	}()

	var event EventPayload
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	scenariosEvaluated.Inc()

	s.mu.RLock()
	scenarios := s.scenarios[event.UserID]
	s.mu.RUnlock()

	triggered := 0
	for _, scenario := range scenarios {
		if !scenario.Enabled {
			continue
		}

		if s.matchesTrigger(scenario.Trigger, event) {
			if s.evaluateConditions(scenario.Conditions, event) {
				go s.executeScenario(scenario, &event)
				scenariosTriggered.WithLabelValues(scenario.ID).Inc()
				triggered++
			}
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"evaluated":  len(scenarios),
		"triggered":  triggered,
		"event_id":   event.EventID,
	})
}

func (s *Service) matchesTrigger(trigger Trigger, event EventPayload) bool {
	if trigger.Type != "device_event" {
		return false
	}

	if trigger.DeviceID != "" && trigger.DeviceID != event.DeviceID {
		return false
	}

	if trigger.Event != "" && trigger.Event != event.EventType {
		return false
	}

	return true
}

func (s *Service) evaluateConditions(conditions []Condition, event EventPayload) bool {
	if len(conditions) == 0 {
		return true
	}

	for _, condition := range conditions {
		if !s.evaluateCondition(condition, event) {
			return false
		}
	}

	return true
}

func (s *Service) evaluateCondition(condition Condition, event EventPayload) bool {
	switch condition.Type {
	case "value_compare":
		value, ok := event.Payload[condition.Property]
		if !ok {
			return false
		}
		return s.compareValues(value, condition.Operator, condition.Value)
	case "device_state":
		// Would query device service for current state
		return true
	default:
		return true
	}
}

func (s *Service) compareValues(actual interface{}, operator string, expected interface{}) bool {
	// Convert to float64 for numeric comparison
	actualFloat, actualOk := toFloat64(actual)
	expectedFloat, expectedOk := toFloat64(expected)

	if actualOk && expectedOk {
		switch operator {
		case "eq":
			return actualFloat == expectedFloat
		case "ne":
			return actualFloat != expectedFloat
		case "gt":
			return actualFloat > expectedFloat
		case "lt":
			return actualFloat < expectedFloat
		case "gte":
			return actualFloat >= expectedFloat
		case "lte":
			return actualFloat <= expectedFloat
		}
	}

	// String comparison
	actualStr := fmt.Sprintf("%v", actual)
	expectedStr := fmt.Sprintf("%v", expected)

	switch operator {
	case "eq":
		return actualStr == expectedStr
	case "ne":
		return actualStr != expectedStr
	case "contains":
		return strings.Contains(actualStr, expectedStr)
	}

	return false
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

func (s *Service) executeScenario(scenario Scenario, event *EventPayload) {
	log.Printf("Executing scenario: %s (%s)", scenario.Name, scenario.ID)

	for _, action := range scenario.Actions {
		if action.Delay > 0 {
			time.Sleep(time.Duration(action.Delay) * time.Second)
		}

		if err := s.executeAction(action, scenario.UserID, event); err != nil {
			log.Printf("Failed to execute action: %v", err)
		} else {
			actionsExecuted.WithLabelValues(action.Type).Inc()
		}
	}
}

func (s *Service) executeAction(action Action, userID string, event *EventPayload) error {
	switch action.Type {
	case "device_command":
		return s.sendDeviceCommand(action)
	case "notification":
		return s.sendNotification(action, userID)
	case "webhook":
		return s.callWebhook(action)
	default:
		log.Printf("Unknown action type: %s", action.Type)
	}
	return nil
}

func (s *Service) sendDeviceCommand(action Action) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"command": action.Command,
		"params":  action.Params,
	})

	url := fmt.Sprintf("%s/devices/%s/command", s.config.DeviceServiceURL, action.DeviceID)
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("device command failed with status %d", resp.StatusCode)
	}

	return nil
}

func (s *Service) sendNotification(action Action, userID string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":  userID,
		"type":     "automation",
		"title":    action.Params["title"],
		"message":  action.Params["message"],
		"priority": action.Params["priority"],
	})

	req, _ := http.NewRequest("POST", s.config.NotificationURL+"/notify", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (s *Service) callWebhook(action Action) error {
	url, ok := action.Params["url"].(string)
	if !ok {
		return fmt.Errorf("webhook URL not specified")
	}

	payload, _ := json.Marshal(action.Params["body"])
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (s *Service) persistScenarios(userID string) {
	ctx := context.Background()
	key := fmt.Sprintf("scenarios:%s", userID)

	s.redis.Del(ctx, key)

	scenarios := s.scenarios[userID]
	for _, scenario := range scenarios {
		scenarioJSON, _ := json.Marshal(scenario)
		s.redis.RPush(ctx, key, scenarioJSON)
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

func main() {
	log.Println("Starting HomeGuard Scenario Engine...")

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

	log.Printf("Scenario Engine listening on port %s", config.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
