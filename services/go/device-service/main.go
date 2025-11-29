package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Config holds the application configuration
type Config struct {
	Port     string
	MongoURL string
	MongoDB  string
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
	config     *Config
	client     *mongo.Client
	db         *mongo.Database
	devices    *mongo.Collection
	commands   *mongo.Collection
	router     *mux.Router
}

func loadConfig() *Config {
	return &Config{
		Port:     getEnv("PORT", "8080"),
		MongoURL: getEnv("MONGO_URL", "mongodb://root:homeguard-mongo-2024@mongodb.homeguard-data:27017/homeguard?authSource=admin"),
		MongoDB:  getEnv("MONGO_DB", "homeguard"),
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

	service := &Service{
		config:     config,
		client:     client,
		db:         db,
		devices:    db.Collection("devices"),
		commands:   db.Collection("device_commands"),
		router:     mux.NewRouter(),
	}

	// Create indexes
	if err := service.createIndexes(ctx); err != nil {
		log.Printf("Warning: failed to create indexes: %v", err)
	}

	return service, nil
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

	filter := bson.M{"user_id": userID}

	// Optional type filter
	if deviceType := r.URL.Query().Get("type"); deviceType != "" {
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

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"devices": devices,
		"count":   len(devices),
	})
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

	// Return updated device
	var device Device
	s.devices.FindOne(ctx, filter).Decode(&device)
	device.Token = ""
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

	// TODO: Publish command to Kafka for device to pick up

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

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"device_id": device.ID,
		"online":    isOnline,
		"status":    device.Status,
		"last_seen": device.LastSeen,
	})
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
