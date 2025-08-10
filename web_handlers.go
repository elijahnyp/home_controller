package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	. "github.com/elijahnyp/home_controller/util"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

// WebSocketMessage represents a message sent over WebSocket
type WebSocketMessage struct {
	Data interface{} `json:"data"`
	Type string      `json:"type"`
}

// WSClient represents a connected WebSocket client
type WSClient struct {
	conn *websocket.Conn
	send chan WebSocketMessage
	hub  *WSHub
}

// WSHub maintains the set of active clients and broadcasts messages
type WSHub struct {
	clients    map[*WSClient]bool
	broadcast  chan WebSocketMessage
	register   chan *WSClient
	unregister chan *WSClient
}

// SystemStatus represents the overall system status
type SystemStatus struct {
	RoomStatuses   []WebRoomStatus   `json:"room_statuses"`
	RecentActivity []ActivityItem    `json:"recent_activity"`
	Detections     []DetectionResult `json:"detections"`
	TotalRooms     int               `json:"total_rooms"`
	OccupiedRooms  int               `json:"occupied_rooms"`
	ActiveMotion   int               `json:"active_motion"`
	TotalCameras   int               `json:"total_cameras"`
}

// WebRoomStatus represents room status for web interface
type WebRoomStatus struct {
	Name            string `json:"name"`
	Occupied        bool   `json:"occupied"`
	Motion          bool   `json:"motion"`
	LastUpdate      int64  `json:"last_update"`
	OccupancyPeriod int    `json:"occupancy_period"`
}

// DetectionResult represents an AI detection result
type DetectionResult struct {
	RoomName   string  `json:"room_name"`
	Label      string  `json:"label"`
	Confidence float32 `json:"confidence"`
	Timestamp  int64   `json:"timestamp"`
}

// ActivityItem represents a system activity
type ActivityItem struct {
	Type      string `json:"type"`
	Room      string `json:"room"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// RoomDetail represents detailed room information
type RoomDetail struct {
	Images     []RoomImage       `json:"images"`
	Detections []DetectionResult `json:"detections"`
	Name       string            `json:"name"`
	Occupied   bool              `json:"occupied"`
	Motion     bool              `json:"motion"`
}

// RoomImage represents a camera image from a room
type RoomImage struct {
	Topic     string `json:"topic"`
	URL       string `json:"url"`
	Timestamp int64  `json:"timestamp"`
}

// Detection represents an object detection in an image
type Detection struct {
	Label      string  `json:"label"`
	Confidence float32 `json:"confidence"`
	X_min      int     `json:"x_min"`
	Y_min      int     `json:"y_min"`
	X_max      int     `json:"x_max"`
	Y_max      int     `json:"y_max"`
}

var wsHub *WSHub

func init() {
	wsHub = NewHub()
	go wsHub.Run()
}

// NewHub creates a new WebSocket hub
func NewHub() *WSHub {
	return &WSHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan WebSocketMessage),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
	}
}

// Run starts the WebSocket hub
func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			Logger.Info().Msg("Client connected to WebSocket")

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				Logger.Info().Msg("Client disconnected from WebSocket")
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// BroadcastUpdate sends an update to all connected clients
func (h *WSHub) BroadcastUpdate(messageType string, data interface{}) {
	select {
	case h.broadcast <- WebSocketMessage{Type: messageType, Data: data}:
	default:
		// Channel is full, skip this update
	}
}

// readPump pumps messages from the websocket connection to the hub
func (c *WSClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		if err := c.conn.Close(); err != nil {
			Logger.Error().Err(err).Msg("Error closing WebSocket connection")
		}
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// writePump pumps messages from the hub to the websocket connection
func (c *WSClient) writePump() {
	defer func() {
		if err := c.conn.Close(); err != nil {
			Logger.Error().Err(err).Msg("Error closing WebSocket connection")
		}
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					Logger.Error().Err(err).Msg("Error writing close message")
				}
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				return
			}
		}
	}
}

// ServeWebSocket handles websocket requests from the peer
func ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		Logger.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	client := &WSClient{
		conn: conn,
		send: make(chan WebSocketMessage, 256),
		hub:  wsHub,
	}

	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}

// APISystemStatus returns the overall system status as JSON
func APISystemStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := SystemStatus{
		TotalRooms:     len(model.Rooms),
		OccupiedRooms:  0,
		ActiveMotion:   0,
		TotalCameras:   0,
		RoomStatuses:   []WebRoomStatus{},
		RecentActivity: []ActivityItem{},
		Detections:     []DetectionResult{},
	}

	// Calculate stats and room statuses
	for _, room := range model.Rooms {
		status.TotalCameras += len(room.Pic_topics)

		// Get room occupancy and motion status
		occupied := false
		motion := false
		lastUpdate := time.Now().Unix()

		// Check occupancy status
		if val, exists := last_occupancy_state[room.Name]; exists && val {
			occupied = true
			status.OccupiedRooms++
		}

		// Check motion status
		for _, topic := range room.Motion_topics {
			if val, exists := last_motion_state[topic]; exists && val {
				motion = true
				status.ActiveMotion++
				break
			}
		}

		status.RoomStatuses = append(status.RoomStatuses, WebRoomStatus{
			Name:            room.Name,
			Occupied:        occupied,
			Motion:          motion,
			LastUpdate:      lastUpdate,
			OccupancyPeriod: int(room.Occupancy_period),
		})
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		Logger.Error().Err(err).Msg("Error encoding system status")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIRoomDetail returns detailed information about a specific room
func APIRoomDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	roomName := r.URL.Query().Get("room")
	if roomName == "" {
		http.Error(w, "Room name required", http.StatusBadRequest)
		return
	}

	detail := RoomDetail{
		Name:       roomName,
		Occupied:   false,
		Motion:     false,
		Images:     []RoomImage{},
		Detections: []DetectionResult{},
	}

	// Find the room to get its pic topics
	for _, room := range model.Rooms {
		if room.Name == roomName {
			// Get room status
			if val, exists := last_occupancy_state[room.Name]; exists {
				detail.Occupied = val
			}

			// Check motion status
			for _, topic := range room.Motion_topics {
				if val, exists := last_motion_state[topic]; exists && val {
					detail.Motion = true
					break
				}
			}

			// Get images and detection results for each camera topic
			for _, topic := range room.Pic_topics {
				// Add image URL
				detail.Images = append(detail.Images, RoomImage{
					Topic:     topic,
					Timestamp: time.Now().Unix(),
					URL:       fmt.Sprintf("/image?id=%s", topic),
				})

				// Get detection results from cache
				if cacheItem, exists := cache[topic]; exists {
					for _, pred := range cacheItem.results.Predictions {
						detail.Detections = append(detail.Detections, DetectionResult{
							RoomName:   roomName,
							Label:      pred.Label,
							Confidence: pred.Confidence,
							Timestamp:  cacheItem.results.Timestamp,
						})
					}
				}
			}
			break
		}
	}

	if err := json.NewEncoder(w).Encode(detail); err != nil {
		Logger.Error().Err(err).Msg("Error encoding room detail")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// RoomDetailHandler serves the room detail page
func RoomDetailHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/static/room.html")
}

// HomeHandler serves the main dashboard page
func HomeHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/static/index.html")
}