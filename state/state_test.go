package state

import (
	"testing"
)

// Mock implementations for testing interfaces

type MockDevice struct {
	config   interface{}
	location string
	status   uint
}

func (m *MockDevice) Status() uint {
	return m.status
}

func (m *MockDevice) State(newState uint) error {
	m.status = newState
	return nil
}

func (m *MockDevice) Location() string {
	return m.location
}

func (m *MockDevice) SetConfig(config any) error {
	m.config = config
	return nil
}

func (m *MockDevice) GetConfig() any {
	return m.config
}

type MockLight struct {
	MockDevice
}

type MockSensor struct {
	MockDevice
}

type MockRoom struct {
	state RoomState
}

func (m *MockRoom) Status() RoomState {
	return m.state
}

func (m *MockRoom) SetState(newState RoomState) error {
	m.state = newState
	return nil
}

func TestDevice_Interface(t *testing.T) {
	device := &MockDevice{
		status:   1,
		location: "living_room",
		config:   map[string]string{"type": "test"},
	}

	// Test Status
	if device.Status() != 1 {
		t.Errorf("Status() = %d, expected 1", device.Status())
	}

	// Test State
	err := device.State(5)
	if err != nil {
		t.Errorf("State(5) returned error: %v", err)
	}
	if device.Status() != 5 {
		t.Errorf("After State(5), Status() = %d, expected 5", device.Status())
	}

	// Test Location
	if device.Location() != "living_room" {
		t.Errorf("Location() = %s, expected 'living_room'", device.Location())
	}

	// Test SetConfig
	newConfig := map[string]int{"brightness": 80}
	err = device.SetConfig(newConfig)
	if err != nil {
		t.Errorf("SetConfig() returned error: %v", err)
	}

	// Test GetConfig
	retrievedConfig := device.GetConfig()
	if retrievedConfig == nil {
		t.Error("GetConfig() returned nil")
	}
}

func TestLight_Interface(t *testing.T) {
	light := &MockLight{
		MockDevice: MockDevice{
			status:   0, // off
			location: "bedroom",
			config:   map[string]interface{}{"brightness": 100, "color": "white"},
		},
	}

	// Test that Light implements Device interface
	var device Device = light

	// Test device functionality through Light
	if device.Status() != 0 {
		t.Errorf("Light Status() = %d, expected 0", device.Status())
	}

	err := device.State(1) // turn on
	if err != nil {
		t.Errorf("Light State(1) returned error: %v", err)
	}

	if device.Status() != 1 {
		t.Errorf("After turning on, Light Status() = %d, expected 1", device.Status())
	}

	if device.Location() != "bedroom" {
		t.Errorf("Light Location() = %s, expected 'bedroom'", device.Location())
	}

	// Test light-specific config
	lightConfig := map[string]interface{}{
		"brightness": 75,
		"color":      "blue",
		"dimmer":     true,
	}

	err = device.SetConfig(lightConfig)
	if err != nil {
		t.Errorf("Light SetConfig() returned error: %v", err)
	}

	retrievedConfig := device.GetConfig()
	if retrievedConfig == nil {
		t.Error("Light GetConfig() returned nil")
	}
}

func TestSensor_Interface(t *testing.T) {
	sensor := &MockSensor{
		MockDevice: MockDevice{
			status:   42, // sensor reading
			location: "kitchen",
			config:   map[string]interface{}{"type": "temperature", "unit": "celsius"},
		},
	}

	// Test that Sensor implements Device interface
	var device Device = sensor

	// Test device functionality through Sensor
	if device.Status() != 42 {
		t.Errorf("Sensor Status() = %d, expected 42", device.Status())
	}

	err := device.State(38) // new reading
	if err != nil {
		t.Errorf("Sensor State(38) returned error: %v", err)
	}

	if device.Status() != 38 {
		t.Errorf("After new reading, Sensor Status() = %d, expected 38", device.Status())
	}

	if device.Location() != "kitchen" {
		t.Errorf("Sensor Location() = %s, expected 'kitchen'", device.Location())
	}

	// Test sensor-specific config
	sensorConfig := map[string]interface{}{
		"type":            "humidity",
		"unit":            "percent",
		"update_interval": 30,
	}

	err = device.SetConfig(sensorConfig)
	if err != nil {
		t.Errorf("Sensor SetConfig() returned error: %v", err)
	}

	retrievedConfig := device.GetConfig()
	if retrievedConfig == nil {
		t.Error("Sensor GetConfig() returned nil")
	}
}

func TestRoom_Interface(t *testing.T) {
	// Create test devices
	light := &MockLight{
		MockDevice: MockDevice{status: 1, location: "living_room"},
	}
	sensor := &MockSensor{
		MockDevice: MockDevice{status: 22, location: "living_room"},
	}

	// Create room state
	initialState := RoomState{
		Lights:   []Light{light},
		Sensors:  []Sensor{sensor},
		Occupied: true,
	}

	room := &MockRoom{state: initialState}

	// Test Status
	state := room.Status()
	if len(state.Lights) != 1 {
		t.Errorf("Room has %d lights, expected 1", len(state.Lights))
	}
	if len(state.Sensors) != 1 {
		t.Errorf("Room has %d sensors, expected 1", len(state.Sensors))
	}
	if !state.Occupied {
		t.Error("Room should be occupied")
	}

	// Test SetState
	newState := RoomState{
		Lights:   []Light{light},
		Sensors:  []Sensor{sensor},
		Occupied: false,
	}

	err := room.SetState(newState)
	if err != nil {
		t.Errorf("Room SetState() returned error: %v", err)
	}

	// Verify state was updated
	updatedState := room.Status()
	if updatedState.Occupied {
		t.Error("Room should not be occupied after SetState")
	}
}

func TestRoomState_Structure(t *testing.T) {
	// Test empty room state
	emptyState := RoomState{}

	if len(emptyState.Lights) != 0 {
		t.Errorf("Empty room should have 0 lights, got %d", len(emptyState.Lights))
	}
	if len(emptyState.Sensors) != 0 {
		t.Errorf("Empty room should have 0 sensors, got %d", len(emptyState.Sensors))
	}
	if emptyState.Occupied {
		t.Error("Empty room should not be occupied by default")
	}

	// Test room state with devices
	light1 := &MockLight{MockDevice: MockDevice{status: 1, location: "room1"}}
	light2 := &MockLight{MockDevice: MockDevice{status: 0, location: "room1"}}
	sensor1 := &MockSensor{MockDevice: MockDevice{status: 25, location: "room1"}}

	roomState := RoomState{
		Lights:   []Light{light1, light2},
		Sensors:  []Sensor{sensor1},
		Occupied: true,
	}

	if len(roomState.Lights) != 2 {
		t.Errorf("Room should have 2 lights, got %d", len(roomState.Lights))
	}
	if len(roomState.Sensors) != 1 {
		t.Errorf("Room should have 1 sensor, got %d", len(roomState.Sensors))
	}
	if !roomState.Occupied {
		t.Error("Room should be occupied")
	}

	// Test that devices in room state work properly
	if roomState.Lights[0].Status() != 1 {
		t.Errorf("Light 0 status = %d, expected 1", roomState.Lights[0].Status())
	}
	if roomState.Lights[1].Status() != 0 {
		t.Errorf("Light 1 status = %d, expected 0", roomState.Lights[1].Status())
	}
	if roomState.Sensors[0].Status() != 25 {
		t.Errorf("Sensor 0 status = %d, expected 25", roomState.Sensors[0].Status())
	}
}

func TestInterfaceCompatibility(t *testing.T) {
	// Test that our mock implementations properly implement the interfaces

	// Test Device interface
	var device Device = &MockDevice{}
	_ = device // Use the variable to avoid "unused" error

	// Test Light interface (which extends Device)
	var light Light = &MockLight{}
	var lightAsDevice Device = light // Should work since Light extends Device
	_ = lightAsDevice

	// Test Sensor interface (which extends Device)
	var sensor Sensor = &MockSensor{}
	var sensorAsDevice Device = sensor // Should work since Sensor extends Device
	_ = sensorAsDevice

	// Test Room interface
	var room Room = &MockRoom{}
	_ = room

	// If we get here without compilation errors, all interfaces are properly implemented
	t.Log("All interfaces are properly implemented")
}
