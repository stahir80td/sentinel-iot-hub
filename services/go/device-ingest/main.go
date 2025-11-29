package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config holds the application configuration
type Config struct {
	Port             string
	KafkaBrokers     []string
	DeviceServiceURL string
}

// DeviceEvent represents an event from a device
type DeviceEvent struct {
	ID        string                 `json:"id"`
	DeviceID  string                 `json:"device_id"`
	UserID    string                 `json:"user_id"`
	EventType string                 `json:"event_type"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// Metrics
var (
	eventsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "device_ingest_events_received_total",
			Help: "Total number of events received",
		},
		[]string{"event_type", "device_id"},
	)
	eventsPublished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "device_ingest_events_published_total",
			Help: "Total number of events published to Kafka",
		},
		[]string{"topic"},
	)
	eventProcessingDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "device_ingest_event_processing_seconds",
			Help:    "Time spent processing events",
			Buckets: prometheus.DefBuckets,
		},
	)
)

func init() {
	prometheus.MustRegister(eventsReceived)
	prometheus.MustRegister(eventsPublished)
	prometheus.MustRegister(eventProcessingDuration)
}

// Service handles device data ingestion
type Service struct {
	config   *Config
	producer sarama.SyncProducer
	router   *mux.Router
	client   *http.Client
}

func loadConfig() *Config {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "homeguard-kafka-kafka-bootstrap.homeguard-messaging:9092"
	}

	return &Config{
		Port:             getEnv("PORT", "8080"),
		KafkaBrokers:     []string{brokers},
		DeviceServiceURL: getEnv("DEVICE_SERVICE_URL", "http://device-service:8080"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// NewService creates a new device ingest service
func NewService(config *Config) (*Service, error) {
	// Configure Kafka producer
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Producer.RequiredAcks = sarama.WaitForAll
	kafkaConfig.Producer.Retry.Max = 3
	kafkaConfig.Producer.Return.Successes = true
	kafkaConfig.Net.DialTimeout = 10 * time.Second
	kafkaConfig.Net.WriteTimeout = 10 * time.Second

	producer, err := sarama.NewSyncProducer(config.KafkaBrokers, kafkaConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	return &Service{
		config:   config,
		producer: producer,
		router:   mux.NewRouter(),
		client:   &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// SetupRoutes configures all HTTP routes
func (s *Service) SetupRoutes() {
	s.router.HandleFunc("/health", s.healthCheck).Methods("GET")
	s.router.Handle("/metrics", promhttp.Handler())

	// Device ingestion endpoints
	s.router.HandleFunc("/ingest/event", s.ingestEvent).Methods("POST")
	s.router.HandleFunc("/ingest/heartbeat", s.ingestHeartbeat).Methods("POST")
	s.router.HandleFunc("/ingest/telemetry", s.ingestTelemetry).Methods("POST")
	s.router.HandleFunc("/ingest/alert", s.ingestAlert).Methods("POST")
}

func (s *Service) healthCheck(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Service) ingestEvent(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		eventProcessingDuration.Observe(time.Since(start).Seconds())
	}()

	// Validate device token
	deviceToken := r.Header.Get("X-Device-Token")
	if deviceToken == "" {
		s.errorResponse(w, http.StatusUnauthorized, "Missing device token")
		return
	}

	deviceInfo, err := s.validateDeviceToken(deviceToken)
	if err != nil {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid device token")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	eventType, _ := payload["event_type"].(string)
	if eventType == "" {
		eventType = "generic"
	}

	event := DeviceEvent{
		ID:        uuid.New().String(),
		DeviceID:  deviceInfo.DeviceID,
		UserID:    deviceInfo.UserID,
		EventType: eventType,
		Timestamp: time.Now(),
		Payload:   payload,
	}

	// Publish to Kafka
	if err := s.publishEvent("device-events", event); err != nil {
		log.Printf("Error publishing event: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to process event")
		return
	}

	eventsReceived.WithLabelValues(eventType, event.DeviceID).Inc()

	s.jsonResponse(w, http.StatusAccepted, map[string]interface{}{
		"id":      event.ID,
		"status":  "accepted",
		"message": "Event received and queued for processing",
	})
}

func (s *Service) ingestHeartbeat(w http.ResponseWriter, r *http.Request) {
	deviceToken := r.Header.Get("X-Device-Token")
	if deviceToken == "" {
		s.errorResponse(w, http.StatusUnauthorized, "Missing device token")
		return
	}

	deviceInfo, err := s.validateDeviceToken(deviceToken)
	if err != nil {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid device token")
		return
	}

	var payload map[string]interface{}
	json.NewDecoder(r.Body).Decode(&payload)

	// Update device heartbeat
	s.updateDeviceHeartbeat(deviceInfo.DeviceID)

	event := DeviceEvent{
		ID:        uuid.New().String(),
		DeviceID:  deviceInfo.DeviceID,
		UserID:    deviceInfo.UserID,
		EventType: "heartbeat",
		Timestamp: time.Now(),
		Payload:   payload,
	}

	// Publish to heartbeats topic
	s.publishEvent("device-heartbeats", event)

	eventsReceived.WithLabelValues("heartbeat", event.DeviceID).Inc()

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) ingestTelemetry(w http.ResponseWriter, r *http.Request) {
	deviceToken := r.Header.Get("X-Device-Token")
	if deviceToken == "" {
		s.errorResponse(w, http.StatusUnauthorized, "Missing device token")
		return
	}

	deviceInfo, err := s.validateDeviceToken(deviceToken)
	if err != nil {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid device token")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	event := DeviceEvent{
		ID:        uuid.New().String(),
		DeviceID:  deviceInfo.DeviceID,
		UserID:    deviceInfo.UserID,
		EventType: "telemetry",
		Timestamp: time.Now(),
		Payload:   payload,
	}

	// Publish to events topic
	s.publishEvent("device-events", event)

	eventsReceived.WithLabelValues("telemetry", event.DeviceID).Inc()

	s.jsonResponse(w, http.StatusAccepted, map[string]interface{}{
		"id":     event.ID,
		"status": "accepted",
	})
}

func (s *Service) ingestAlert(w http.ResponseWriter, r *http.Request) {
	deviceToken := r.Header.Get("X-Device-Token")
	if deviceToken == "" {
		s.errorResponse(w, http.StatusUnauthorized, "Missing device token")
		return
	}

	deviceInfo, err := s.validateDeviceToken(deviceToken)
	if err != nil {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid device token")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	event := DeviceEvent{
		ID:        uuid.New().String(),
		DeviceID:  deviceInfo.DeviceID,
		UserID:    deviceInfo.UserID,
		EventType: "alert",
		Timestamp: time.Now(),
		Payload:   payload,
	}

	// Publish to alerts topic (higher priority)
	s.publishEvent("device-alerts", event)

	eventsReceived.WithLabelValues("alert", event.DeviceID).Inc()

	s.jsonResponse(w, http.StatusAccepted, map[string]interface{}{
		"id":      event.ID,
		"status":  "accepted",
		"message": "Alert received and queued for immediate processing",
	})
}

type DeviceInfo struct {
	DeviceID string
	UserID   string
}

func (s *Service) validateDeviceToken(token string) (*DeviceInfo, error) {
	body, _ := json.Marshal(map[string]string{"token": token})
	req, _ := http.NewRequest("POST", s.config.DeviceServiceURL+"/internal/devices/validate-token",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid token")
	}

	var result struct {
		Valid    bool   `json:"valid"`
		DeviceID string `json:"device_id"`
		UserID   string `json:"user_id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if !result.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return &DeviceInfo{
		DeviceID: result.DeviceID,
		UserID:   result.UserID,
	}, nil
}

func (s *Service) updateDeviceHeartbeat(deviceID string) {
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/internal/devices/%s/heartbeat", s.config.DeviceServiceURL, deviceID), nil)
	s.client.Do(req)
}

func (s *Service) publishEvent(topic string, event DeviceEvent) error {
	eventBytes, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic:     topic,
		Key:       sarama.StringEncoder(event.DeviceID),
		Value:     sarama.ByteEncoder(eventBytes),
		Timestamp: event.Timestamp,
	}

	_, _, err = s.producer.SendMessage(msg)
	if err != nil {
		return err
	}

	eventsPublished.WithLabelValues(topic).Inc()
	return nil
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
	log.Println("Starting HomeGuard Device Ingest Service...")

	config := loadConfig()
	service, err := NewService(config)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}
	defer service.producer.Close()

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

	log.Printf("Device Ingest Service listening on port %s", config.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
