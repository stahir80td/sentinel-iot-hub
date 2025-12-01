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
	"strings"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	deviceListCacheTTL   = 30 * time.Second
	deviceStatusCacheTTL = 15 * time.Second
)

// Prometheus metrics
var (
	deviceCommands = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "device_commands_total",
			Help: "Total number of device commands sent",
		},
		[]string{"command", "device_type"},
	)
	deviceOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "device_operations_total",
			Help: "Total number of device operations",
		},
		[]string{"operation"},
	)
	activityEventsPublished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "activity_events_published_total",
			Help: "Total number of activity events published",
		},
		[]string{"source"},
	)
	cacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "device_cache_hits_total",
			Help: "Total number of cache hits",
		},
	)
	cacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "device_cache_misses_total",
			Help: "Total number of cache misses",
		},
	)
)

func init() {
	prometheus.MustRegister(deviceCommands)
	prometheus.MustRegister(deviceOperations)
	prometheus.MustRegister(activityEventsPublished)
	prometheus.MustRegister(cacheHits)
	prometheus.MustRegister(cacheMisses)
}

// Config holds the application configuration
type Config struct {
	Port                   string
	MongoURL               string
	MongoDB                string
	RedisURL               string
	KafkaBrokers           []string
	KafkaCommandsTopic     string
	NotificationServiceURL string
}

// ActivityEvent represents an activity event for the activity stream
type ActivityEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Icon      string    `json:"icon"`
	Action    string    `json:"action"`
	Details   string    `json:"details"`
	UserID    string    `json:"user_id"`
	DeviceID  string    `json:"device_id,omitempty"`
	Severity  string    `json:"severity"`
}

// Device represents an IoT device
type Device struct {
	ID           string                 `json:"id" bson:"_id"`
	UserID       string                 `json:"user_id" bson:"user_id"`
	Name         string                 `json:"name" bson:"name"`
	Type         string                 `json:"type" bson:"type"`
	Manufacturer string                 `json:"manufacturer" bson:"manufacturer"`
	Model        string                 `json:"model" bson:"model"`
	Location     string                 `json:"location" bson:"location"`
	Status       string                 `json:"status" bson:"status"`
	Online       bool                   `json:"online" bson:"online"`
	Token        string                 `json:"token,omitempty" bson:"token"`
	Config       map[string]interface{} `json:"config" bson:"config"`
	Metadata     map[string]interface{} `json:"metadata" bson:"metadata"`
	LastSeen     time.Time              `json:"last_seen" bson:"last_seen"`
	CreatedAt    time.Time              `json:"created_at" bson:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at" bson:"updated_at"`
}

// DeviceCommand represents a command to send to a device
type DeviceCommand struct {
	ID        string                 `json:"id" bson:"_id"`
	DeviceID  string                 `json:"device_id" bson:"device_id"`
	UserID    string                 `json:"user_id" bson:"user_id"`
	Command   string                 `json:"command" bson:"command"`
	Payload   map[string]interface{} `json:"payload" bson:"payload"`
	Status    string                 `json:"status" bson:"status"`
	Response  map[string]interface{} `json:"response,omitempty" bson:"response"`
	CreatedAt time.Time              `json:"created_at" bson:"created_at"`
	UpdatedAt time.Time              `json:"updated_at" bson:"updated_at"`
}

// Service handles device-related operations
type Service struct {
	config        *Config
	client        *mongo.Client
	db            *mongo.Database
	devices       *mongo.Collection
	commands      *mongo.Collection
	redis         *redis.Client
	kafkaProducer sarama.SyncProducer
	router        *mux.Router
}

func loadConfig() *Config {
	kafkaBrokers := getEnv("KAFKA_BROKERS", "iot-kafka.sandbox:9092")
	return &Config{
		Port:                   getEnv("PORT", "8080"),
		MongoURL:               getEnv("MONGO_URL", "mongodb://root:homeguard-mongo-2024@mongodb.homeguard-data:27017/homeguard?authSource=admin"),
		MongoDB:                getEnv("MONGO_DB", "homeguard"),
		RedisURL:               getEnv("REDIS_URL", "redis://iot-redis.sandbox:6379"),
		KafkaBrokers:           strings.Split(kafkaBrokers, ","),
		KafkaCommandsTopic:     getEnv("KAFKA_COMMANDS_TOPIC", "device-events"),
		NotificationServiceURL: getEnv("NOTIFICATION_SERVICE_URL", "http://iot-notification-service.sandbox:8080"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// NewService creates a new device service
func NewService(config *Config) (*Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(config.MongoURL))
	if err != nil {
		log.Printf("Warning: failed to connect to MongoDB: %v - service will run without database", err)
		return &Service{
			config: config,
			router: mux.NewRouter(),
		}, nil
	}

	if err := client.Ping(ctx, nil); err != nil {
		log.Printf("Warning: failed to ping MongoDB: %v - service will run without database", err)
		return &Service{
			config: config,
			router: mux.NewRouter(),
		}, nil
	}

	db := client.Database(config.MongoDB)

	// Connect to Redis
	opt, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		log.Printf("Warning: failed to parse Redis URL: %v - service will run without cache", err)
	}
	redisClient := redis.NewClient(opt)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: failed to connect to Redis: %v - service will run without cache", err)
		redisClient = nil
	} else {
		log.Println("Connected to Redis cache")
	}

	// Connect to Kafka
	var kafkaProducer sarama.SyncProducer
	if len(config.KafkaBrokers) > 0 && config.KafkaBrokers[0] != "" {
		kafkaConfig := sarama.NewConfig()
		kafkaConfig.Producer.RequiredAcks = sarama.WaitForAll
		kafkaConfig.Producer.Retry.Max = 3
		kafkaConfig.Producer.Return.Successes = true
		kafkaConfig.Net.DialTimeout = 10 * time.Second

		kafkaProducer, err = sarama.NewSyncProducer(config.KafkaBrokers, kafkaConfig)
		if err != nil {
			log.Printf("Warning: failed to create Kafka producer: %v - service will run without Kafka", err)
		} else {
			log.Printf("Connected to Kafka brokers: %v", config.KafkaBrokers)
		}
	}

	service := &Service{
		config:        config,
		client:        client,
		db:            db,
		devices:       db.Collection("devices"),
		commands:      db.Collection("device_commands"),
		redis:         redisClient,
		kafkaProducer: kafkaProducer,
		router:        mux.NewRouter(),
	}

	// Create indexes
	if err := service.createIndexes(ctx); err != nil {
		log.Printf("Warning: failed to create indexes: %v", err)
	}

	return service, nil
}

// Cache key helpers
func (s *Service) deviceListCacheKey(userID string, deviceType string) string {
	if deviceType != "" {
		return fmt.Sprintf("devices:list:%s:%s", userID, deviceType)
	}
	return fmt.Sprintf("devices:list:%s", userID)
}

func (s *Service) deviceCacheKey(deviceID string) string {
	return fmt.Sprintf("devices:item:%s", deviceID)
}

// Cache invalidation
func (s *Service) invalidateUserDeviceCache(ctx context.Context, userID string) {
	if s.redis == nil {
		return
	}
	pattern := fmt.Sprintf("devices:list:%s*", userID)
	keys, err := s.redis.Keys(ctx, pattern).Result()
	if err != nil {
		log.Printf("Warning: failed to get cache keys: %v", err)
		return
	}
	if len(keys) > 0 {
		s.redis.Del(ctx, keys...)
	}
}

func (s *Service) invalidateDeviceCache(ctx context.Context, deviceID string) {
	if s.redis == nil {
		return
	}
	s.redis.Del(ctx, s.deviceCacheKey(deviceID))
}

// KafkaEvent represents an event to publish to Kafka
type KafkaEvent struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	DeviceID  string                 `json:"device_id"`
	UserID    string                 `json:"user_id"`
	Command   string                 `json:"command"`
	Payload   map[string]interface{} `json:"payload"`
	Device    *Device                `json:"device,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// publishToKafka publishes an event to Kafka
func (s *Service) publishToKafka(event *KafkaEvent) error {
	if s.kafkaProducer == nil {
		log.Printf("[KAFKA] Producer not available, skipping publish for event %s", event.ID)
		return nil
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: s.config.KafkaCommandsTopic,
		Key:   sarama.StringEncoder(event.DeviceID),
		Value: sarama.ByteEncoder(data),
	}

	partition, offset, err := s.kafkaProducer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to send message to Kafka: %w", err)
	}

	log.Printf("[KAFKA] Published event %s to topic %s (partition=%d, offset=%d)",
		event.ID, s.config.KafkaCommandsTopic, partition, offset)

	return nil
}

// publishActivity sends an activity event to the notification service
func (s *Service) publishActivity(source, icon, action, details, userID, deviceID, severity string) {
	event := ActivityEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    source,
		Icon:      icon,
		Action:    action,
		Details:   details,
		UserID:    userID,
		DeviceID:  deviceID,
		Severity:  severity,
	}

	// Log for Grafana/Loki
	log.Printf("[ACTIVITY] source=%s action=%s details=%s user=%s device=%s severity=%s",
		source, action, details, userID, deviceID, severity)

	// Send to notification service asynchronously
	go func() {
		data, err := json.Marshal(event)
		if err != nil {
			log.Printf("Error marshaling activity event: %v", err)
			return
		}

		resp, err := http.Post(
			s.config.NotificationServiceURL+"/activity",
			"application/json",
			bytes.NewReader(data),
		)
		if err != nil {
			log.Printf("Error publishing activity: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			log.Printf("Activity publish returned status: %d", resp.StatusCode)
		} else {
			activityEventsPublished.WithLabelValues(source).Inc()
		}
	}()
}

func (s *Service) createIndexes(ctx context.Context) error {
	// Device indexes
	deviceIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "type", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "token", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
	}
	_, err := s.devices.Indexes().CreateMany(ctx, deviceIndexes)
	if err != nil {
		return err
	}

	// Command indexes
	commandIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "device_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
	}
	_, err = s.commands.Indexes().CreateMany(ctx, commandIndexes)
	return err
}

// SetupRoutes configures all HTTP routes
func (s *Service) SetupRoutes() {
	s.router.HandleFunc("/health", s.healthCheck).Methods("GET")
	s.router.Handle("/metrics", promhttp.Handler())

	// Device CRUD
	s.router.HandleFunc("/devices", s.listDevices).Methods("GET")
	s.router.HandleFunc("/devices", s.createDevice).Methods("POST")
	s.router.HandleFunc("/devices/{id}", s.getDevice).Methods("GET")
	s.router.HandleFunc("/devices/{id}", s.updateDevice).Methods("PUT")
	s.router.HandleFunc("/devices/{id}", s.patchDevice).Methods("PATCH")
	s.router.HandleFunc("/devices/{id}", s.deleteDevice).Methods("DELETE")

	// Device operations
	s.router.HandleFunc("/devices/{id}/command", s.sendCommand).Methods("POST")
	s.router.HandleFunc("/devices/{id}/status", s.getDeviceStatus).Methods("GET")
	s.router.HandleFunc("/devices/{id}/events", s.getDeviceEvents).Methods("GET")

	// Internal endpoints (called by other services)
	s.router.HandleFunc("/internal/devices/validate-token", s.validateDeviceToken).Methods("POST")
	s.router.HandleFunc("/internal/devices/{id}/heartbeat", s.updateHeartbeat).Methods("POST")
}

func (s *Service) healthCheck(w http.ResponseWriter, r *http.Request) {
	// Check if MongoDB is connected
	if s.client != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := s.client.Ping(ctx, nil); err != nil {
			s.jsonResponse(w, http.StatusOK, map[string]string{
				"status":   "degraded",
				"database": "unavailable",
			})
			return
		}
	} else {
		s.jsonResponse(w, http.StatusOK, map[string]string{
			"status":   "degraded",
			"database": "not connected",
		})
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Service) listDevices(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	deviceType := r.URL.Query().Get("type")
	cacheKey := s.deviceListCacheKey(userID, deviceType)

	// Try cache first
	if s.redis != nil {
		cached, err := s.redis.Get(ctx, cacheKey).Result()
		if err == nil {
			var response map[string]interface{}
			if json.Unmarshal([]byte(cached), &response) == nil {
				log.Printf("[CACHE] HIT key=%s", cacheKey)
				cacheHits.Inc()
				s.jsonResponse(w, http.StatusOK, response)
				return
			}
		}
		log.Printf("[CACHE] MISS key=%s", cacheKey)
		cacheMisses.Inc()
	}

	filter := bson.M{"user_id": userID}
	if deviceType != "" {
		filter["type"] = deviceType
	}

	cursor, err := s.devices.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		log.Printf("Error listing devices: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to list devices")
		return
	}
	defer cursor.Close(ctx)

	var devices []Device
	if err := cursor.All(ctx, &devices); err != nil {
		log.Printf("Error decoding devices: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to decode devices")
		return
	}

	// Remove tokens from response
	for i := range devices {
		devices[i].Token = ""
	}

	response := map[string]interface{}{
		"devices": devices,
		"count":   len(devices),
	}

	// Cache the result
	if s.redis != nil {
		if data, err := json.Marshal(response); err == nil {
			s.redis.Set(ctx, cacheKey, data, deviceListCacheTTL)
			log.Printf("Cached devices list: %s", cacheKey)
		}
	}

	s.jsonResponse(w, http.StatusOK, response)
}

func (s *Service) createDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	var device Device
	if err := json.NewDecoder(r.Body).Decode(&device); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if device.Name == "" || device.Type == "" {
		s.errorResponse(w, http.StatusBadRequest, "Name and type are required")
		return
	}

	// Set defaults
	device.ID = uuid.New().String()
	device.UserID = userID
	device.Status = "inactive"
	device.Online = false
	device.Token = uuid.New().String() // Generate device token
	device.CreatedAt = time.Now()
	device.UpdatedAt = time.Now()
	device.LastSeen = time.Time{}

	if device.Config == nil {
		device.Config = make(map[string]interface{})
	}
	if device.Metadata == nil {
		device.Metadata = make(map[string]interface{})
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.devices.InsertOne(ctx, device)
	if err != nil {
		log.Printf("Error creating device: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to create device")
		return
	}

	// Invalidate user's device list cache
	s.invalidateUserDeviceCache(ctx, userID)

	// Publish activity events
	deviceOperations.WithLabelValues("create").Inc()
	s.publishActivity("mongodb", "📦", "Device Created",
		fmt.Sprintf("New %s device '%s' registered in MongoDB", device.Type, device.Name),
		userID, device.ID, "info")
	s.publishActivity("redis", "🗑️", "Cache Invalidated",
		fmt.Sprintf("Device list cache cleared for user after adding '%s'", device.Name),
		userID, device.ID, "info")

	s.jsonResponse(w, http.StatusCreated, device)
}

func (s *Service) getDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	vars := mux.Vars(r)
	deviceID := vars["id"]

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var device Device
	filter := bson.M{"_id": deviceID}
	if userID != "" {
		filter["user_id"] = userID
	}

	err := s.devices.FindOne(ctx, filter).Decode(&device)
	if err == mongo.ErrNoDocuments {
		s.errorResponse(w, http.StatusNotFound, "Device not found")
		return
	}
	if err != nil {
		log.Printf("Error getting device: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to get device")
		return
	}

	device.Token = "" // Don't expose token
	s.jsonResponse(w, http.StatusOK, device)
}

func (s *Service) updateDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	vars := mux.Vars(r)
	deviceID := vars["id"]

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Only allow certain fields to be updated
	allowedFields := map[string]bool{
		"name": true, "location": true, "config": true, "metadata": true,
	}
	updateDoc := bson.M{"updated_at": time.Now()}
	for key, value := range updates {
		if allowedFields[key] {
			updateDoc[key] = value
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	filter := bson.M{"_id": deviceID, "user_id": userID}
	result, err := s.devices.UpdateOne(ctx, filter, bson.M{"$set": updateDoc})
	if err != nil {
		log.Printf("Error updating device: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to update device")
		return
	}

	if result.MatchedCount == 0 {
		s.errorResponse(w, http.StatusNotFound, "Device not found")
		return
	}

	// Invalidate caches
	s.invalidateUserDeviceCache(ctx, userID)
	s.invalidateDeviceCache(ctx, deviceID)

	// Return updated device
	var device Device
	s.devices.FindOne(ctx, filter).Decode(&device)
	device.Token = ""

	// Publish activity events
	deviceOperations.WithLabelValues("update").Inc()
	s.publishActivity("mongodb", "📝", "Device Updated",
		fmt.Sprintf("Device '%s' configuration updated in MongoDB", device.Name),
		userID, deviceID, "info")
	s.publishActivity("redis", "🗑️", "Cache Invalidated",
		fmt.Sprintf("Cache cleared for device '%s'", device.Name),
		userID, deviceID, "info")

	s.jsonResponse(w, http.StatusOK, device)
}

func (s *Service) patchDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	vars := mux.Vars(r)
	deviceID := vars["id"]

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Allow status and online fields for PATCH (partial updates)
	allowedFields := map[string]bool{
		"name": true, "location": true, "config": true, "metadata": true,
		"status": true, "online": true,
	}
	updateDoc := bson.M{"updated_at": time.Now()}
	for key, value := range updates {
		if allowedFields[key] {
			updateDoc[key] = value
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	filter := bson.M{"_id": deviceID, "user_id": userID}
	result, err := s.devices.UpdateOne(ctx, filter, bson.M{"$set": updateDoc})
	if err != nil {
		log.Printf("Error patching device: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to update device")
		return
	}

	if result.MatchedCount == 0 {
		s.errorResponse(w, http.StatusNotFound, "Device not found")
		return
	}

	// Invalidate caches
	s.invalidateUserDeviceCache(ctx, userID)
	s.invalidateDeviceCache(ctx, deviceID)

	// Return updated device
	var device Device
	s.devices.FindOne(ctx, filter).Decode(&device)
	device.Token = ""

	// Publish activity events
	deviceOperations.WithLabelValues("patch").Inc()
	s.publishActivity("mongodb", "📝", "Device Patched",
		fmt.Sprintf("Device '%s' partially updated in MongoDB", device.Name),
		userID, deviceID, "info")

	s.jsonResponse(w, http.StatusOK, device)
}

func (s *Service) deleteDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	vars := mux.Vars(r)
	deviceID := vars["id"]

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	filter := bson.M{"_id": deviceID, "user_id": userID}
	result, err := s.devices.DeleteOne(ctx, filter)
	if err != nil {
		log.Printf("Error deleting device: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to delete device")
		return
	}

	if result.DeletedCount == 0 {
		s.errorResponse(w, http.StatusNotFound, "Device not found")
		return
	}

	// Invalidate caches
	s.invalidateUserDeviceCache(ctx, userID)
	s.invalidateDeviceCache(ctx, deviceID)

	// Publish activity events
	deviceOperations.WithLabelValues("delete").Inc()
	s.publishActivity("mongodb", "🗑️", "Device Removed",
		fmt.Sprintf("Device '%s' deleted from MongoDB", deviceID),
		userID, deviceID, "warning")
	s.publishActivity("redis", "🗑️", "Cache Invalidated",
		"Device cache entries removed",
		userID, deviceID, "info")

	s.jsonResponse(w, http.StatusOK, map[string]string{"message": "Device deleted"})
}

func (s *Service) sendCommand(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		s.errorResponse(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	vars := mux.Vars(r)
	deviceID := vars["id"]

	var req struct {
		Command string                 `json:"command"`
		Payload map[string]interface{} `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Command == "" {
		s.errorResponse(w, http.StatusBadRequest, "Command is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Verify device belongs to user
	var device Device
	err := s.devices.FindOne(ctx, bson.M{"_id": deviceID, "user_id": userID}).Decode(&device)
	if err == mongo.ErrNoDocuments {
		s.errorResponse(w, http.StatusNotFound, "Device not found")
		return
	}
	if err != nil {
		log.Printf("Error finding device: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to find device")
		return
	}

	// Create command record
	command := DeviceCommand{
		ID:        uuid.New().String(),
		DeviceID:  deviceID,
		UserID:    userID,
		Command:   req.Command,
		Payload:   req.Payload,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err = s.commands.InsertOne(ctx, command)
	if err != nil {
		log.Printf("Error creating command: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to create command")
		return
	}

	// For demo purposes, immediately update the device config based on the command
	// In production, this would be handled by the actual device via Kafka
	configUpdate := bson.M{}
	switch req.Command {
	case "turn_on":
		configUpdate["config.power_on"] = true
	case "turn_off":
		configUpdate["config.power_on"] = false
	case "set_brightness":
		if brightness, ok := req.Payload["brightness"]; ok {
			configUpdate["config.brightness"] = brightness
		}
	case "set_temperature":
		if temp, ok := req.Payload["temperature"]; ok {
			configUpdate["config.target_temp"] = temp
		}
	case "lock":
		configUpdate["config.locked"] = true
	case "unlock":
		configUpdate["config.locked"] = false
	case "arm":
		configUpdate["config.mode"] = "armed"
	case "disarm":
		configUpdate["config.mode"] = "disarmed"
	}

	// Generate human-readable action description
	var actionDesc, stateDesc string
	var icon string
	switch req.Command {
	case "turn_on":
		actionDesc = "Power On"
		stateDesc = fmt.Sprintf("Device '%s' powered on", device.Name)
		icon = "💡"
	case "turn_off":
		actionDesc = "Power Off"
		stateDesc = fmt.Sprintf("Device '%s' powered off", device.Name)
		icon = "🔌"
	case "set_brightness":
		actionDesc = "Brightness Changed"
		stateDesc = fmt.Sprintf("Device '%s' brightness set to %v%%", device.Name, req.Payload["brightness"])
		icon = "🔆"
	case "set_temperature":
		actionDesc = "Temperature Set"
		stateDesc = fmt.Sprintf("Device '%s' temperature set to %v°F", device.Name, req.Payload["temperature"])
		icon = "🌡️"
	case "lock":
		actionDesc = "Door Locked"
		stateDesc = fmt.Sprintf("Smart lock '%s' is now LOCKED", device.Name)
		icon = "🔒"
	case "unlock":
		actionDesc = "Door Unlocked"
		stateDesc = fmt.Sprintf("Smart lock '%s' is now UNLOCKED", device.Name)
		icon = "🔓"
	case "arm":
		actionDesc = "Alarm Armed"
		stateDesc = fmt.Sprintf("Alarm '%s' has been armed", device.Name)
		icon = "🚨"
	case "disarm":
		actionDesc = "Alarm Disarmed"
		stateDesc = fmt.Sprintf("Alarm '%s' has been disarmed", device.Name)
		icon = "✅"
	default:
		actionDesc = "Command Executed"
		stateDesc = fmt.Sprintf("Command '%s' sent to device '%s'", req.Command, device.Name)
		icon = "⚙️"
	}

	// Publish activity: Device command received
	deviceCommands.WithLabelValues(req.Command, device.Type).Inc()
	s.publishActivity("device", icon, actionDesc, stateDesc, userID, deviceID, "info")

	if len(configUpdate) > 0 {
		configUpdate["updated_at"] = time.Now()
		configUpdate["last_seen"] = time.Now()
		_, err = s.devices.UpdateOne(ctx, bson.M{"_id": deviceID}, bson.M{"$set": configUpdate})
		if err != nil {
			log.Printf("Error updating device config: %v", err)
		}

		// Publish activity: MongoDB updated
		s.publishActivity("mongodb", "📝", "State Persisted",
			fmt.Sprintf("Device state saved to MongoDB for '%s'", device.Name),
			userID, deviceID, "info")

		// Invalidate caches after command updates device
		s.invalidateUserDeviceCache(ctx, userID)
		s.invalidateDeviceCache(ctx, deviceID)

		// Publish activity: Redis cache invalidated
		s.publishActivity("redis", "🗑️", "Cache Invalidated",
			fmt.Sprintf("Redis cache cleared for device '%s'", device.Name),
			userID, deviceID, "info")
	}

	// Publish to Kafka for event-processor to consume
	kafkaEvent := &KafkaEvent{
		ID:        command.ID,
		Type:      "device_command",
		DeviceID:  deviceID,
		UserID:    userID,
		Command:   req.Command,
		Payload:   req.Payload,
		Device:    &device,
		Timestamp: time.Now(),
	}

	if err := s.publishToKafka(kafkaEvent); err != nil {
		log.Printf("Error publishing to Kafka: %v", err)
		s.publishActivity("kafka", "❌", "Publish Failed",
			fmt.Sprintf("Failed to publish command '%s' to Kafka: %v", req.Command, err),
			userID, deviceID, "warning")
	} else {
		s.publishActivity("kafka", "📨", "Event Published",
			fmt.Sprintf("Command '%s' published to Kafka topic '%s'", req.Command, s.config.KafkaCommandsTopic),
			userID, deviceID, "info")
	}

	s.jsonResponse(w, http.StatusAccepted, command)
}

func (s *Service) getDeviceStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	vars := mux.Vars(r)
	deviceID := vars["id"]

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var device Device
	filter := bson.M{"_id": deviceID}
	if userID != "" {
		filter["user_id"] = userID
	}

	err := s.devices.FindOne(ctx, filter).Decode(&device)
	if err == mongo.ErrNoDocuments {
		s.errorResponse(w, http.StatusNotFound, "Device not found")
		return
	}
	if err != nil {
		log.Printf("Error getting device: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to get device")
		return
	}

	// Check if device is online (last seen within 2 minutes)
	isOnline := time.Since(device.LastSeen) < 2*time.Minute

	// Build detailed status based on device type
	detailedStatus := map[string]interface{}{
		"device_id":   device.ID,
		"name":        device.Name,
		"type":        device.Type,
		"online":      isOnline,
		"status":      device.Status,
		"last_seen":   device.LastSeen,
		"location":    device.Location,
		"config":      device.Config,
	}

	// Add human-readable state based on device type and config
	if device.Config != nil {
		switch device.Type {
		case "light":
			if powerOn, ok := device.Config["power_on"].(bool); ok {
				if powerOn {
					detailedStatus["state"] = "on"
				} else {
					detailedStatus["state"] = "off"
				}
			}
			if brightness, ok := device.Config["brightness"].(float64); ok {
				detailedStatus["brightness"] = int(brightness)
			}
		case "thermostat":
			if temp, ok := device.Config["target_temp"].(float64); ok {
				detailedStatus["target_temperature"] = int(temp)
			}
			if mode, ok := device.Config["mode"].(string); ok {
				detailedStatus["mode"] = mode
			}
		case "smart_lock":
			if locked, ok := device.Config["locked"].(bool); ok {
				if locked {
					detailedStatus["state"] = "locked"
				} else {
					detailedStatus["state"] = "unlocked"
				}
			}
		case "camera":
			if recording, ok := device.Config["recording"].(bool); ok {
				detailedStatus["recording"] = recording
			}
			if motionDetection, ok := device.Config["motion_detection"].(bool); ok {
				detailedStatus["motion_detection"] = motionDetection
			}
		case "smart_plug":
			if powerOn, ok := device.Config["power_on"].(bool); ok {
				if powerOn {
					detailedStatus["state"] = "on"
				} else {
					detailedStatus["state"] = "off"
				}
			}
		case "alarm":
			if mode, ok := device.Config["mode"].(string); ok {
				detailedStatus["alarm_mode"] = mode
			}
		}
	}

	s.jsonResponse(w, http.StatusOK, detailedStatus)
}

func (s *Service) getDeviceEvents(w http.ResponseWriter, r *http.Request) {
	// This would typically query ScyllaDB for events
	// For now, return a placeholder
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"events": []interface{}{},
		"count":  0,
	})
}

func (s *Service) validateDeviceToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var device Device
	err := s.devices.FindOne(ctx, bson.M{"token": req.Token}).Decode(&device)
	if err == mongo.ErrNoDocuments {
		s.errorResponse(w, http.StatusUnauthorized, "Invalid device token")
		return
	}
	if err != nil {
		log.Printf("Error validating token: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to validate token")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"valid":     true,
		"device_id": device.ID,
		"user_id":   device.UserID,
	})
}

func (s *Service) updateHeartbeat(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	deviceID := vars["id"]

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	update := bson.M{
		"$set": bson.M{
			"last_seen":  time.Now(),
			"online":     true,
			"status":     "active",
			"updated_at": time.Now(),
		},
	}

	_, err := s.devices.UpdateOne(ctx, bson.M{"_id": deviceID}, update)
	if err != nil {
		log.Printf("Error updating heartbeat: %v", err)
		s.errorResponse(w, http.StatusInternalServerError, "Failed to update heartbeat")
		return
	}

	// Invalidate device cache on heartbeat (status may have changed)
	s.invalidateDeviceCache(ctx, deviceID)

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
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
	log.Println("Starting HomeGuard Device Service...")

	config := loadConfig()
	service, err := NewService(config)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}
	defer func() {
		if service.client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			service.client.Disconnect(ctx)
		}
		if service.redis != nil {
			service.redis.Close()
		}
		if service.kafkaProducer != nil {
			service.kafkaProducer.Close()
		}
	}()

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

	log.Printf("Device Service listening on port %s", config.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
