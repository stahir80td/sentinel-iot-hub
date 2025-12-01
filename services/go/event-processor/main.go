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

	"github.com/IBM/sarama"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config holds the application configuration
type Config struct {
	Port               string
	KafkaBrokers       []string
	TimescaleDBURL     string
	ScyllaDBHosts      []string
	ConsumerGroup      string
	Topics             []string
	NotificationURL    string
	ScenarioEngineURL  string
	N8NWebhookURL      string
}

// ActivityEvent for publishing to the activity stream
type ActivityEvent struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Icon      string `json:"icon"`
	Action    string `json:"action"`
	Details   string `json:"details"`
	UserID    string `json:"user_id"`
	DeviceID  string `json:"device_id,omitempty"`
	Severity  string `json:"severity"`
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
	eventsProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "event_processor_events_processed_total",
			Help: "Total number of events processed",
		},
		[]string{"event_type", "topic"},
	)
	eventProcessingErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "event_processor_errors_total",
			Help: "Total number of processing errors",
		},
		[]string{"event_type", "error_type"},
	)
	eventProcessingDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "event_processor_processing_seconds",
			Help:    "Time spent processing events",
			Buckets: prometheus.DefBuckets,
		},
	)
	consumerLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "event_processor_consumer_lag",
			Help: "Consumer lag per partition",
		},
		[]string{"topic", "partition"},
	)
)

func init() {
	prometheus.MustRegister(eventsProcessed)
	prometheus.MustRegister(eventProcessingErrors)
	prometheus.MustRegister(eventProcessingDuration)
	prometheus.MustRegister(consumerLag)
}

// Service handles event processing
type Service struct {
	config        *Config
	consumerGroup sarama.ConsumerGroup
	router        *mux.Router
	client        *http.Client
	ready         chan bool
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func loadConfig() *Config {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "homeguard-kafka-kafka-bootstrap.homeguard-messaging:9092"
	}

	topics := os.Getenv("KAFKA_TOPICS")
	if topics == "" {
		topics = "device-events,device-alerts,device-heartbeats"
	}

	scyllaHosts := os.Getenv("SCYLLA_HOSTS")
	if scyllaHosts == "" {
		scyllaHosts = "scylladb.homeguard-data:9042"
	}

	return &Config{
		Port:              getEnv("PORT", "8080"),
		KafkaBrokers:      strings.Split(brokers, ","),
		TimescaleDBURL:    getEnv("TIMESCALEDB_URL", "postgres://homeguard:homeguard@timescaledb.homeguard-data:5432/homeguard_analytics?sslmode=disable"),
		ScyllaDBHosts:     strings.Split(scyllaHosts, ","),
		ConsumerGroup:     getEnv("CONSUMER_GROUP", "event-processor"),
		Topics:            strings.Split(topics, ","),
		NotificationURL:   getEnv("NOTIFICATION_SERVICE_URL", "http://iot-notification-service:8080"),
		ScenarioEngineURL: getEnv("SCENARIO_ENGINE_URL", "http://iot-scenario-engine:8080"),
		N8NWebhookURL:     getEnv("N8N_WEBHOOK_URL", "http://iot-n8n:5678/webhook/device-event"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// NewService creates a new event processor service
func NewService(config *Config) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Configure Kafka consumer
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	kafkaConfig.Consumer.Offsets.Initial = sarama.OffsetNewest
	kafkaConfig.Consumer.Return.Errors = true
	kafkaConfig.Net.DialTimeout = 10 * time.Second

	consumerGroup, err := sarama.NewConsumerGroup(config.KafkaBrokers, config.ConsumerGroup, kafkaConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create consumer group: %w", err)
	}

	return &Service{
		config:        config,
		consumerGroup: consumerGroup,
		router:        mux.NewRouter(),
		client:        &http.Client{Timeout: 5 * time.Second},
		ready:         make(chan bool),
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

// SetupRoutes configures HTTP routes
func (s *Service) SetupRoutes() {
	s.router.HandleFunc("/health", s.healthCheck).Methods("GET")
	s.router.HandleFunc("/ready", s.readyCheck).Methods("GET")
	s.router.Handle("/metrics", promhttp.Handler())
}

func (s *Service) healthCheck(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Service) readyCheck(w http.ResponseWriter, r *http.Request) {
	select {
	case <-s.ready:
		s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ready"})
	default:
		s.jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
	}
}

// ConsumerGroupHandler implements sarama.ConsumerGroupHandler
type ConsumerGroupHandler struct {
	service *Service
}

func (h *ConsumerGroupHandler) Setup(session sarama.ConsumerGroupSession) error {
	close(h.service.ready)
	return nil
}

func (h *ConsumerGroupHandler) Cleanup(session sarama.ConsumerGroupSession) error {
	h.service.ready = make(chan bool)
	return nil
}

func (h *ConsumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}

			start := time.Now()

			if err := h.service.processMessage(msg); err != nil {
				log.Printf("Error processing message: %v", err)
				eventProcessingErrors.WithLabelValues(msg.Topic, "processing_error").Inc()
			} else {
				eventsProcessed.WithLabelValues(msg.Topic, msg.Topic).Inc()
			}

			eventProcessingDuration.Observe(time.Since(start).Seconds())
			session.MarkMessage(msg, "")

		case <-session.Context().Done():
			return nil
		}
	}
}

func (s *Service) processMessage(msg *sarama.ConsumerMessage) error {
	var event DeviceEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	log.Printf("Processing event: %s, type: %s, device: %s", event.ID, event.EventType, event.DeviceID)

	// Route to appropriate processor based on topic
	switch msg.Topic {
	case "device-events":
		return s.processDeviceEvent(event)
	case "device-alerts":
		return s.processAlert(event)
	case "device-heartbeats":
		return s.processHeartbeat(event)
	default:
		return s.processDeviceEvent(event)
	}
}

func (s *Service) processDeviceEvent(event DeviceEvent) error {
	// Publish activity: Kafka received event
	s.publishActivity("kafka", "📨", "Event Received",
		fmt.Sprintf("Kafka consumed event: %s from device %s", event.EventType, event.DeviceID),
		event.UserID, event.DeviceID, "info")

	// Store in TimescaleDB for analytics
	if err := s.storeInTimescaleDB(event); err != nil {
		log.Printf("Failed to store in TimescaleDB: %v", err)
	} else {
		s.publishActivity("timescaledb", "📊", "Event Stored",
			fmt.Sprintf("TimescaleDB stored event %s for analytics", event.EventType),
			event.UserID, event.DeviceID, "info")
	}

	// Store in ScyllaDB for fast lookup
	if err := s.storeInScyllaDB(event); err != nil {
		log.Printf("Failed to store in ScyllaDB: %v", err)
	}

	// Call N8N webhook for workflow automation
	s.triggerN8NWorkflow(event)

	// Trigger scenario engine for automation rules
	s.triggerScenarioEngine(event)

	return nil
}

func (s *Service) processAlert(event DeviceEvent) error {
	// Publish activity: Alert received
	s.publishActivity("kafka", "🚨", "Alert Received",
		fmt.Sprintf("Alert from device %s: %s", event.DeviceID, event.EventType),
		event.UserID, event.DeviceID, "alert")

	// Store the alert
	if err := s.storeInScyllaDB(event); err != nil {
		log.Printf("Failed to store alert in ScyllaDB: %v", err)
	}

	// Send notification
	s.sendNotification(event)

	// Trigger N8N for alert workflow
	s.triggerN8NWorkflow(event)

	// Trigger scenario engine for alert-based automations
	s.triggerScenarioEngine(event)

	return nil
}

func (s *Service) processHeartbeat(event DeviceEvent) error {
	// Update device last seen timestamp
	// This would update a cache/DB with the latest heartbeat
	log.Printf("Heartbeat from device %s at %v", event.DeviceID, event.Timestamp)
	return nil
}

func (s *Service) storeInTimescaleDB(event DeviceEvent) error {
	// In production, this would use a connection pool
	// For now, we'll log the intent
	log.Printf("Storing event %s in TimescaleDB", event.ID)
	return nil
}

func (s *Service) storeInScyllaDB(event DeviceEvent) error {
	// In production, this would use gocql
	// For now, we'll log the intent
	log.Printf("Storing event %s in ScyllaDB", event.ID)
	return nil
}

func (s *Service) sendNotification(event DeviceEvent) {
	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":    event.UserID,
		"device_id":  event.DeviceID,
		"event_type": event.EventType,
		"title":      fmt.Sprintf("Alert from device %s", event.DeviceID),
		"message":    fmt.Sprintf("Event type: %s", event.EventType),
		"priority":   "high",
		"payload":    event.Payload,
	})

	req, _ := http.NewRequest("POST", s.config.NotificationURL+"/notify", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("Failed to send notification: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		log.Printf("Notification service returned status %d", resp.StatusCode)
	}
}

func (s *Service) triggerScenarioEngine(event DeviceEvent) {
	payload, _ := json.Marshal(map[string]interface{}{
		"event_id":   event.ID,
		"device_id":  event.DeviceID,
		"user_id":    event.UserID,
		"event_type": event.EventType,
		"timestamp":  event.Timestamp,
		"payload":    event.Payload,
	})

	req, _ := http.NewRequest("POST", s.config.ScenarioEngineURL+"/evaluate", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("Failed to trigger scenario engine: %v", err)
		return
	}
	defer resp.Body.Close()
}

// triggerN8NWorkflow sends events to N8N for workflow automation
func (s *Service) triggerN8NWorkflow(event DeviceEvent) {
	payload, _ := json.Marshal(map[string]interface{}{
		"event_id":   event.ID,
		"device_id":  event.DeviceID,
		"user_id":    event.UserID,
		"event_type": event.EventType,
		"timestamp":  event.Timestamp,
		"payload":    event.Payload,
	})

	log.Printf("[N8N] Triggering workflow for event: %s, type: %s", event.ID, event.EventType)

	go func() {
		req, _ := http.NewRequest("POST", s.config.N8NWebhookURL, strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("[N8N] Failed to trigger workflow: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
			log.Printf("[N8N] Workflow triggered successfully for event %s", event.ID)
			s.publishActivity("n8n", "⚙️", "Workflow Triggered",
				fmt.Sprintf("N8N processing %s event from device %s", event.EventType, event.DeviceID),
				event.UserID, event.DeviceID, "info")
		} else {
			log.Printf("[N8N] Workflow returned status %d", resp.StatusCode)
		}
	}()
}

// publishActivity sends activity events to the notification service for the activity stream
func (s *Service) publishActivity(source, icon, action, details, userID, deviceID, severity string) {
	activity := ActivityEvent{
		ID:        fmt.Sprintf("act-%d", time.Now().UnixNano()),
		Timestamp: time.Now().Format(time.RFC3339),
		Source:    source,
		Icon:      icon,
		Action:    action,
		Details:   details,
		UserID:    userID,
		DeviceID:  deviceID,
		Severity:  severity,
	}

	log.Printf("[ACTIVITY] source=%s action=%s details=%s user=%s device=%s severity=%s",
		source, action, details, userID, deviceID, severity)

	go func() {
		data, _ := json.Marshal(activity)
		req, _ := http.NewRequest("POST", s.config.NotificationURL+"/activity", strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("[ACTIVITY] Failed to publish: %v", err)
			return
		}
		defer resp.Body.Close()
	}()
}

func (s *Service) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Start begins consuming messages
func (s *Service) Start() {
	handler := &ConsumerGroupHandler{service: s}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			if err := s.consumerGroup.Consume(s.ctx, s.config.Topics, handler); err != nil {
				if err == sarama.ErrClosedConsumerGroup {
					return
				}
				log.Printf("Error from consumer: %v", err)
			}

			if s.ctx.Err() != nil {
				return
			}
		}
	}()

	// Handle consumer errors
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case err, ok := <-s.consumerGroup.Errors():
				if !ok {
					return
				}
				log.Printf("Consumer error: %v", err)
			case <-s.ctx.Done():
				return
			}
		}
	}()
}

func (s *Service) Stop() {
	s.cancel()
	s.consumerGroup.Close()
	s.wg.Wait()
}

func main() {
	log.Println("Starting HomeGuard Event Processor...")

	config := loadConfig()
	service, err := NewService(config)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}

	service.SetupRoutes()
	service.Start()

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

		log.Println("Shutting down...")
		service.Stop()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Event Processor listening on port %s", config.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
