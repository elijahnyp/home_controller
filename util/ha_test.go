package util

import (
	"encoding/json"
	"testing"
)

func TestConstructHAAdvertisement(t *testing.T) {
	name := "living_room"
	stateTopic := "hab/living_room/occupancy"

	advertisement := ConstructHAAdvertisement(name, stateTopic)

	// Test basic fields
	if advertisement.Name != name {
		t.Errorf("Name = %s, expected %s", advertisement.Name, name)
	}

	if advertisement.StateTopic != stateTopic {
		t.Errorf("StateTopic = %s, expected %s", advertisement.StateTopic, stateTopic)
	}

	// Test payload fields
	if advertisement.PayloadOn != "true" {
		t.Errorf("PayloadOn = %s, expected 'true'", advertisement.PayloadOn)
	}

	if advertisement.PayloadOff != "false" {
		t.Errorf("PayloadOff = %s, expected 'false'", advertisement.PayloadOff)
	}

	// Test device class
	if advertisement.DeviceClass != "occupancy" {
		t.Errorf("DeviceClass = %s, expected 'occupancy'", advertisement.DeviceClass)
	}

	// Test platform
	if advertisement.Platform != "binary_sensor" {
		t.Errorf("Platform = %s, expected 'binary_sensor'", advertisement.Platform)
	}

	// Test unique ID
	expectedUniqueID := "occupancy_sensor-" + name
	if advertisement.UniqueID != expectedUniqueID {
		t.Errorf("UniqueID = %s, expected %s", advertisement.UniqueID, expectedUniqueID)
	}

	// Test QoS
	if advertisement.Qos != 0 {
		t.Errorf("Qos = %d, expected 0", advertisement.Qos)
	}

	// Test availability
	if len(advertisement.HAAvdvertisementAvailability) != 1 {
		t.Errorf("Expected 1 availability item, got %d", len(advertisement.HAAvdvertisementAvailability))
	} else {
		avail := advertisement.HAAvdvertisementAvailability[0]
		if avail.Topic != "hab/online" {
			t.Errorf("Availability topic = %s, expected 'hab/online'", avail.Topic)
		}
		if avail.PayloadAvailable != "online" {
			t.Errorf("PayloadAvailable = %s, expected 'online'", avail.PayloadAvailable)
		}
		if avail.PayloadNotAvailable != "offline" {
			t.Errorf("PayloadNotAvailable = %s, expected 'offline'", avail.PayloadNotAvailable)
		}
	}

	// Test device specification
	if advertisement.Device.Name != "occupancy_controller" {
		t.Errorf("Device name = %s, expected 'occupancy_controller'", advertisement.Device.Name)
	}

	if len(advertisement.Device.Identifiers) != 1 || advertisement.Device.Identifiers[0] != "occupancy_controller" {
		t.Errorf("Device identifiers = %v, expected ['occupancy_controller']", advertisement.Device.Identifiers)
	}
}

func TestHAAdvertisement_ToJson(t *testing.T) {
	// Create test advertisement
	advertisement := HAAdvertisement{
		Name:       "test_room",
		StateTopic: "hab/test/occupancy",
		PayloadOn:  "true",
		PayloadOff: "false",
		HAAvdvertisementAvailability: []HAAvdvertisementAvailability{
			{
				Topic:               "hab/online",
				PayloadAvailable:    "online",
				PayloadNotAvailable: "offline",
			},
		},
		Qos:         0,
		UniqueID:    "occupancy_sensor-test_room",
		DeviceClass: "occupancy",
		Platform:    "binary_sensor",
		Device: HADeviceSpec{
			Name:        "occupancy_controller",
			Identifiers: []string{"occupancy_controller"},
		},
	}

	// Convert to JSON
	jsonStr := advertisement.ToJson()

	if jsonStr == "" {
		t.Error("ToJson() should not return empty string")
	}

	// Verify JSON is valid by unmarshaling it back
	var unmarshaled HAAdvertisement
	err := json.Unmarshal([]byte(jsonStr), &unmarshaled)
	if err != nil {
		t.Errorf("ToJson() produced invalid JSON: %v", err)
	}

	// Verify key fields are preserved
	if unmarshaled.Name != advertisement.Name {
		t.Errorf("JSON roundtrip failed for Name: got %s, expected %s", unmarshaled.Name, advertisement.Name)
	}

	if unmarshaled.StateTopic != advertisement.StateTopic {
		t.Errorf("JSON roundtrip failed for StateTopic: got %s, expected %s", unmarshaled.StateTopic, advertisement.StateTopic)
	}

	if unmarshaled.DeviceClass != advertisement.DeviceClass {
		t.Errorf("JSON roundtrip failed for DeviceClass: got %s, expected %s", unmarshaled.DeviceClass, advertisement.DeviceClass)
	}
}

func TestAdvertiseHA(t *testing.T) {
	// Create test rooms
	rooms := []Room{
		{
			Name:            "living_room",
			Occupancy_topic: "hab/living_room/occupancy",
		},
		{
			Name:            "kitchen",
			Occupancy_topic: "hab/kitchen/occupancy",
		},
		{
			Name:            "bedroom",
			Occupancy_topic: "", // Empty topic should be skipped
		},
	}

	// Create mock MQTT client
	mockClient := &MockMQTTClient{}

	// Call AdvertiseHA
	AdvertiseHA(rooms, mockClient)

	// Verify publish calls
	expectedPublishCount := 2 // Only rooms with non-empty occupancy topics
	if len(mockClient.publishCalls) != expectedPublishCount {
		t.Errorf("Expected %d publish calls, got %d", expectedPublishCount, len(mockClient.publishCalls))
	}

	// Check specific publish calls
	publishedTopics := make(map[string]string) // topic -> payload
	for _, call := range mockClient.publishCalls {
		publishedTopics[call.Topic] = call.Payload.(string) //nolint:errcheck // test helper
	}

	// Verify living room advertisement
	livingRoomTopic := "homeassistant/binary_sensor/living_room/occupancy/config"
	if payload, exists := publishedTopics[livingRoomTopic]; !exists {
		t.Errorf("Expected publish to %s", livingRoomTopic)
	} else {
		// Verify payload is valid JSON
		var advertisement HAAdvertisement
		err := json.Unmarshal([]byte(payload), &advertisement)
		if err != nil {
			t.Errorf("Invalid JSON payload for living room: %v", err)
		}
		if advertisement.Name != "living_room" {
			t.Errorf("Living room advertisement name = %s, expected 'living_room'", advertisement.Name)
		}
	}

	// Verify kitchen advertisement
	kitchenTopic := "homeassistant/binary_sensor/kitchen/occupancy/config"
	if payload, exists := publishedTopics[kitchenTopic]; !exists {
		t.Errorf("Expected publish to %s", kitchenTopic)
	} else {
		// Verify payload is valid JSON
		var advertisement HAAdvertisement
		err := json.Unmarshal([]byte(payload), &advertisement)
		if err != nil {
			t.Errorf("Invalid JSON payload for kitchen: %v", err)
		}
		if advertisement.Name != "kitchen" {
			t.Errorf("Kitchen advertisement name = %s, expected 'kitchen'", advertisement.Name)
		}
	}

	// Verify bedroom was skipped (empty occupancy topic)
	bedroomTopic := "homeassistant/binary_sensor/bedroom/occupancy/config"
	if _, exists := publishedTopics[bedroomTopic]; exists {
		t.Errorf("Bedroom should not be advertised (empty occupancy topic)")
	}
}

func TestHADeviceSpec(t *testing.T) {
	device := HADeviceSpec{
		Name:        "test_device",
		Identifiers: []string{"id1", "id2", "id3"},
	}

	// Test JSON marshaling
	jsonData, err := json.Marshal(device)
	if err != nil {
		t.Errorf("Failed to marshal HADeviceSpec: %v", err)
	}

	// Test JSON unmarshaling
	var unmarshaled HADeviceSpec
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Errorf("Failed to unmarshal HADeviceSpec: %v", err)
	}

	// Verify fields
	if unmarshaled.Name != device.Name {
		t.Errorf("Device name = %s, expected %s", unmarshaled.Name, device.Name)
	}

	if len(unmarshaled.Identifiers) != len(device.Identifiers) {
		t.Errorf("Device identifiers length = %d, expected %d", len(unmarshaled.Identifiers), len(device.Identifiers))
	}

	for i, id := range device.Identifiers {
		if unmarshaled.Identifiers[i] != id {
			t.Errorf("Device identifier[%d] = %s, expected %s", i, unmarshaled.Identifiers[i], id)
		}
	}
}

func TestHAAvdvertisementAvailability(t *testing.T) {
	availability := HAAvdvertisementAvailability{
		Topic:               "test/availability",
		PayloadAvailable:    "up",
		PayloadNotAvailable: "down",
	}

	// Test JSON marshaling
	jsonData, err := json.Marshal(availability)
	if err != nil {
		t.Errorf("Failed to marshal HAAvdvertisementAvailability: %v", err)
	}

	// Test JSON unmarshaling
	var unmarshaled HAAvdvertisementAvailability
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Errorf("Failed to unmarshal HAAvdvertisementAvailability: %v", err)
	}

	// Verify fields
	if unmarshaled.Topic != availability.Topic {
		t.Errorf("Availability topic = %s, expected %s", unmarshaled.Topic, availability.Topic)
	}

	if unmarshaled.PayloadAvailable != availability.PayloadAvailable {
		t.Errorf("PayloadAvailable = %s, expected %s", unmarshaled.PayloadAvailable, availability.PayloadAvailable)
	}

	if unmarshaled.PayloadNotAvailable != availability.PayloadNotAvailable {
		t.Errorf("PayloadNotAvailable = %s, expected %s", unmarshaled.PayloadNotAvailable, availability.PayloadNotAvailable)
	}
}

func TestAdvertiseHA_ErrorHandling(t *testing.T) {
	// Test with mock client that simulates errors
	mockClient := &MockMQTTClient{}

	// Create a room with occupancy topic
	rooms := []Room{
		{
			Name:            "test_room",
			Occupancy_topic: "hab/test/occupancy",
		},
	}

	// This should not panic even if there are MQTT errors
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AdvertiseHA should not panic: %v", r)
		}
	}()

	AdvertiseHA(rooms, mockClient)

	// Verify at least one publish call was made
	if len(mockClient.publishCalls) != 1 {
		t.Errorf("Expected 1 publish call, got %d", len(mockClient.publishCalls))
	}
}
