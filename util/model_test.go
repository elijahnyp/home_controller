package util

import (
	"testing"
	"time"
)

func TestModel_FindRoomByTopic(t *testing.T) {
	model := Model{
		Rooms: []Room{
			{
				Name:            "living_room",
				Occupancy_topic: "hab/living_room/occupancy",
				Motion_topics:   []string{"hab/living_room/motion1", "hab/living_room/motion2"},
				Pic_topics:      []string{"hab/living_room/camera1"},
				Door_topics:     []string{"hab/living_room/door1"},
			},
			{
				Name:            "kitchen",
				Occupancy_topic: "hab/kitchen/occupancy",
				Motion_topics:   []string{"hab/kitchen/motion1"},
				Pic_topics:      []string{"hab/kitchen/camera1", "hab/kitchen/camera2"},
			},
		},
	}

	tests := []struct {
		name     string
		topic    string
		expected string
	}{
		{"Occupancy topic", "hab/living_room/occupancy", "living_room"},
		{"Motion topic", "hab/living_room/motion1", "living_room"},
		{"Picture topic", "hab/kitchen/camera2", "kitchen"},
		{"Door topic", "hab/living_room/door1", "living_room"},
		{"Unknown topic", "hab/unknown/topic", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.FindRoomByTopic(tt.topic)
			if result != tt.expected {
				t.Errorf("FindRoomByTopic(%s) = %s, expected %s", tt.topic, result, tt.expected)
			}
		})
	}
}

func TestModel_FindTopicType(t *testing.T) {
	model := Model{
		Rooms: []Room{
			{
				Name:            "test_room",
				Occupancy_topic: "hab/test/occupancy",
				Motion_topics:   []string{"hab/test/motion"},
				Pic_topics:      []string{"hab/test/camera"},
				Door_topics:     []string{"hab/test/door"},
			},
		},
	}

	tests := []struct {
		name     string
		topic    string
		expected int
	}{
		{"Occupancy type", "hab/test/occupancy", OCCUPANCY},
		{"Motion type", "hab/test/motion", MOTION},
		{"Picture type", "hab/test/camera", PIC},
		{"Door type", "hab/test/door", DOOR},
		{"Unknown type", "hab/unknown", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.FindTopicType(tt.topic)
			if result != tt.expected {
				t.Errorf("FindTopicType(%s) = %d, expected %d", tt.topic, result, tt.expected)
			}
		})
	}
}

func TestModel_FindOccupancyTopicByRoom(t *testing.T) {
	model := Model{
		Rooms: []Room{
			{Name: "living_room", Occupancy_topic: "hab/living_room/occupancy"},
			{Name: "kitchen", Occupancy_topic: "hab/kitchen/occupancy"},
		},
	}

	tests := []struct {
		name     string
		room     string
		expected string
	}{
		{"Valid room", "living_room", "hab/living_room/occupancy"},
		{"Another valid room", "kitchen", "hab/kitchen/occupancy"},
		{"Invalid room", "bedroom", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.FindOccupancyTopicByRoom(tt.room)
			if result != tt.expected {
				t.Errorf("FindOccupancyTopicByRoom(%s) = %s, expected %s", tt.room, result, tt.expected)
			}
		})
	}
}

func TestModel_RoomOccupancyPeriod(t *testing.T) {
	// Setup config for default value
	Config.Set("occupancy_period_default", int64(300))

	model := Model{
		Rooms: []Room{
			{Name: "living_room", Occupancy_period: 120},
			{Name: "kitchen"}, // Occupancy_period is 0, should use default
		},
	}

	tests := []struct {
		name     string
		room     string
		expected int64
	}{
		{"Room with custom period", "living_room", 120},
		{"Room with default period", "kitchen", 300},
		{"Non-existent room", "bedroom", 300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.RoomOccupancyPeriod(tt.room)
			if result != tt.expected {
				t.Errorf("RoomOccupancyPeriod(%s) = %d, expected %d", tt.room, result, tt.expected)
			}
		})
	}
}

func TestModel_SubscribeTopics(t *testing.T) {
	model := Model{
		Rooms: []Room{
			{
				Motion_topics: []string{"motion1", "motion2"},
				Pic_topics:    []string{"camera1"},
				Door_topics:   []string{"door1"},
			},
			{
				Motion_topics: []string{"motion3"},
				Pic_topics:    []string{"camera2", "camera3"},
				Door_topics:   []string{},
			},
		},
	}

	topics := model.SubscribeTopics()
	expected := []string{"motion1", "motion2", "camera1", "door1", "motion3", "camera2", "camera3"}

	if len(topics) != len(expected) {
		t.Errorf("SubscribeTopics() returned %d topics, expected %d", len(topics), len(expected))
	}

	// Convert to map for easier comparison
	topicMap := make(map[string]bool)
	for _, topic := range topics {
		topicMap[topic] = true
	}

	for _, expectedTopic := range expected {
		if !topicMap[expectedTopic] {
			t.Errorf("SubscribeTopics() missing expected topic: %s", expectedTopic)
		}
	}
}

func TestRoomStatus_Methods(t *testing.T) {
	status := RoomStatus{}

	// Test initial state
	if status.GetLastOccupied() != 0 {
		t.Errorf("Initial last_occupied should be 0, got %d", status.GetLastOccupied())
	}
	if status.GetMotionState() != false {
		t.Error("Initial motion_state should be false")
	}

	// Test Occupied method
	now := time.Now().Unix()
	status.Occupied()
	if status.occupied != true {
		t.Error("Occupied() should set occupied to true")
	}
	if status.GetLastOccupied() < now {
		t.Error("Occupied() should update last_occupied timestamp")
	}

	// Test Unoccupied method
	status.Unoccupied()
	if status.occupied != false {
		t.Error("Unoccupied() should set occupied to false")
	}

	// Test Motion method
	status.Motion(true)
	if status.GetMotionState() != true {
		t.Error("Motion(true) should set motion_state to true")
	}
	if status.occupied != true {
		t.Error("Motion() should also set occupied to true")
	}

	status.Motion(false)
	if status.GetMotionState() != false {
		t.Error("Motion(false) should set motion_state to false")
	}
}

func TestModel_BuildModel(t *testing.T) {
	// Setup test config
	testModelConfig := map[string]interface{}{
		"rooms": []map[string]interface{}{
			{
				"name":             "test_room",
				"occupancy_topic":  "hab/test/occupancy",
				"occupancy_period": 120,
				"motion_topics":    []string{"hab/test/motion"},
				"pic_topics":       []string{"hab/test/camera"},
			},
		},
	}

	Config.Set("model", testModelConfig)

	model := &Model{}
	err := model.BuildModel()

	if err != nil {
		t.Errorf("BuildModel() returned error: %v", err)
	}

	if len(model.Rooms) != 1 {
		t.Errorf("BuildModel() should create 1 room, got %d", len(model.Rooms))
	}

	room := model.Rooms[0]
	if room.Name != "test_room" {
		t.Errorf("Room name should be 'test_room', got %s", room.Name)
	}
	if room.Occupancy_topic != "hab/test/occupancy" {
		t.Errorf("Room occupancy_topic should be 'hab/test/occupancy', got %s", room.Occupancy_topic)
	}
	if room.Occupancy_period != 120 {
		t.Errorf("Room occupancy_period should be 120, got %d", room.Occupancy_period)
	}
}

func TestLocation_GetCoordinates(t *testing.T) {
	location := Location{
		Lat:  42.3601,
		Lon:  -71.0589,
		Name: "Boston",
	}

	lat, lon := location.GetCoordinates()
	if lat != 42.3601 {
		t.Errorf("GetCoordinates() latitude = %f, expected %f", lat, 42.3601)
	}
	if lon != -71.0589 {
		t.Errorf("GetCoordinates() longitude = %f, expected %f", lon, -71.0589)
	}
}

func TestModel_ModelStatus(t *testing.T) {
	model := Model{}

	// Initialize model_status if not already done
	if model_status == nil {
		model_status = &ModelStatus{
			Room_status: make(map[string]RoomStatus),
		}
	}

	status := model.ModelStatus()
	if status == nil {
		t.Error("ModelStatus() should return non-nil status")
		return // Exit early to avoid nil pointer dereference
	}
	if status.Room_status == nil {
		t.Error("ModelStatus().Room_status should be initialized")
	}
}

func TestModel_UpdateRoomStatus(t *testing.T) {
	model := Model{}

	// Initialize model_status
	model_status = &ModelStatus{
		Room_status: make(map[string]RoomStatus),
	}

	testStatus := RoomStatus{
		last_occupied: time.Now().Unix(),
		motion_state:  true,
		occupied:      true,
	}

	model.UpdateRoomStatus("test_room", testStatus)

	retrievedStatus, exists := model_status.Room_status["test_room"]
	if !exists {
		t.Error("UpdateRoomStatus() should add room status to model_status")
	}
	if retrievedStatus.GetMotionState() != true {
		t.Error("Updated room status should have correct motion_state")
	}
	if retrievedStatus.occupied != true {
		t.Error("Updated room status should have correct occupied state")
	}
}
