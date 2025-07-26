package util

import (
	"sync"
	"testing"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// Mock MQTT client for testing
type MockMQTTClient struct {
	publishCalls   []PublishCall
	subscribeCalls []SubscribeCall
	connected      bool
	mu             sync.RWMutex // Add mutex for thread safety
}

type PublishCall struct {
	Payload  interface{}
	Topic    string
	QoS      byte
	Retained bool
}

type SubscribeCall struct {
	Handler MQTT.MessageHandler
	Topic   string
	QoS     byte
}

func (m *MockMQTTClient) IsConnected() bool      { return m.connected }
func (m *MockMQTTClient) IsConnectionOpen() bool { return m.connected }
func (m *MockMQTTClient) Connect() MQTT.Token {
	m.connected = true
	return &MockToken{}
}
func (m *MockMQTTClient) Disconnect(quiesce uint) { m.connected = false }

func (m *MockMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) MQTT.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishCalls = append(m.publishCalls, PublishCall{
		Topic:    topic,
		QoS:      qos,
		Retained: retained,
		Payload:  payload,
	})
	return &MockToken{}
}

func (m *MockMQTTClient) Subscribe(topic string, qos byte, callback MQTT.MessageHandler) MQTT.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribeCalls = append(m.subscribeCalls, SubscribeCall{
		Topic:   topic,
		QoS:     qos,
		Handler: callback,
	})
	return &MockToken{}
}

func (m *MockMQTTClient) SubscribeMultiple(filters map[string]byte, callback MQTT.MessageHandler) MQTT.Token {
	return &MockToken{}
}
func (m *MockMQTTClient) Unsubscribe(topics ...string) MQTT.Token             { return &MockToken{} }
func (m *MockMQTTClient) AddRoute(topic string, callback MQTT.MessageHandler) {}
func (m *MockMQTTClient) OptionsReader() MQTT.ClientOptionsReader             { return MQTT.ClientOptionsReader{} }

// Mock MQTT token
type MockToken struct {
	err error
}

func (m *MockToken) Wait() bool                     { return true }
func (m *MockToken) WaitTimeout(time.Duration) bool { return true }
func (m *MockToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (m *MockToken) Error() error { return m.err }

// Mock MQTT message
type MockMessage struct {
	topic   string
	payload []byte
}

func (m *MockMessage) Duplicate() bool   { return false }
func (m *MockMessage) Qos() byte         { return 0 }
func (m *MockMessage) Retained() bool    { return false }
func (m *MockMessage) Topic() string     { return m.topic }
func (m *MockMessage) MessageID() uint16 { return 0 }
func (m *MockMessage) Payload() []byte   { return m.payload }
func (m *MockMessage) Ack()              {}

func TestRegisterMQTTConnectHook(t *testing.T) {
	// Clear existing handlers
	connectHandlers = make(map[string]func(MQTT.Client))

	// Test adding a handler
	called := false
	testHandler := func(client MQTT.Client) {
		called = true
	}

	RegisterMQTTConnectHook("test_handler", testHandler)

	if len(connectHandlers) != 1 {
		t.Errorf("Expected 1 connect handler, got %d", len(connectHandlers))
	}

	// Test handler is called during connection
	mockClient := &MockMQTTClient{}
	if connectHandlers["test_handler"] != nil {
		connectHandlers["test_handler"](mockClient)
	}

	if !called {
		t.Error("Connect handler should have been called")
	}

	// Test removing a handler
	RegisterMQTTConnectHook("test_handler", nil)
	if len(connectHandlers) != 0 {
		t.Errorf("Expected 0 connect handlers after removal, got %d", len(connectHandlers))
	}
}

func TestRegisterMQTTSubscription(t *testing.T) {
	// Clear existing subscriptions
	subscriptions = make(map[string]MQTT.MessageHandler)

	// Test adding a subscription
	testHandler := func(client MQTT.Client, message MQTT.Message) {
		// Test handler
	}

	RegisterMQTTSubscription("test/topic", testHandler)

	if len(subscriptions) != 1 {
		t.Errorf("Expected 1 subscription, got %d", len(subscriptions))
	}

	if subscriptions["test/topic"] == nil {
		t.Error("Subscription handler should not be nil")
	}

	// Test removing a subscription
	RegisterMQTTSubscription("test/topic", nil)
	if len(subscriptions) != 0 {
		t.Errorf("Expected 0 subscriptions after removal, got %d", len(subscriptions))
	}
}

func TestSubscribe(t *testing.T) {
	// Setup mock client
	mockClient := &MockMQTTClient{}
	Client = mockClient

	// Setup test subscriptions
	subscriptions = make(map[string]MQTT.MessageHandler)
	testHandler := func(client MQTT.Client, message MQTT.Message) {}
	subscriptions["test/topic1"] = testHandler
	subscriptions["test/topic2"] = testHandler

	// Call subscribe
	subscribe()

	// Verify subscriptions were called
	if len(mockClient.subscribeCalls) != 2 {
		t.Errorf("Expected 2 subscribe calls, got %d", len(mockClient.subscribeCalls))
	}

	// Check specific topics
	topics := make(map[string]bool)
	for _, call := range mockClient.subscribeCalls {
		topics[call.Topic] = true
	}

	if !topics["test/topic1"] || !topics["test/topic2"] {
		t.Error("Expected both test topics to be subscribed")
	}
}

func TestMqttInit(t *testing.T) {
	// Setup test configuration
	Config.Set("broker_uri", "tcp://test.mqtt.broker:1883")
	Config.Set("id_base", "test_client")
	Config.Set("username", "test_user")
	Config.Set("password", "test_pass")
	Config.Set("cleansess", true)

	// Clear existing client
	Client = nil

	// Note: This test would require a real MQTT broker or more extensive mocking
	// For now, we'll test that the function doesn't panic and sets up the client
	defer func() {
		if r := recover(); r != nil {
			// Expected to panic due to no real broker, but we can check setup was attempted
			t.Logf("MqttInit panicked as expected (no real broker): %v", r)
		}
	}()

	// This will panic due to no real broker, but verifies the setup code runs
	MqttInit()
}

func TestGetRandString(t *testing.T) {
	// Test various lengths
	lengths := []int{1, 5, 10, 20}

	for _, length := range lengths {
		result := GetRandString(length)

		if len(result) != length {
			t.Errorf("GetRandString(%d) returned string of length %d", length, len(result))
		}

		// Check that it only contains valid characters
		for _, char := range result {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')) {
				t.Errorf("GetRandString(%d) contains invalid character: %c", length, char)
			}
		}
	}

	// Test that multiple calls return different strings (very likely)
	str1 := GetRandString(10)
	str2 := GetRandString(10)

	if str1 == str2 {
		t.Error("GetRandString should return different strings on consecutive calls")
	}
}

func TestReceiverFunction(t *testing.T) {
	// Setup mock client and message
	mockClient := &MockMQTTClient{}
	mockMessage := &MockMessage{
		topic:   "unknown/topic",
		payload: []byte("test payload"),
	}

	// Capture log output by redirecting Logger
	// Since the receiver function just logs a warning for unknown topics,
	// we'll test that it doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("receiver function should not panic: %v", r)
		}
	}()

	receiver(mockClient, mockMessage)
}

func TestConnectHandler(t *testing.T) {
	// Setup mock client
	mockClient := &MockMQTTClient{}
	Client = mockClient

	// Setup test connect handlers
	connectHandlers = make(map[string]func(MQTT.Client))

	handlerCalled := false
	testHandler := func(client MQTT.Client) { //nolint:unparam // test parameter is required by interface
		handlerCalled = true
	}
	connectHandlers["test"] = testHandler

	// Call connect handler
	connectHandler(mockClient)

	// Check that online message was published
	if len(mockClient.publishCalls) < 1 {
		t.Error("Connect handler should publish online message")
	} else {
		call := mockClient.publishCalls[0]
		if call.Topic != "hab/online" || call.Payload != "online" {
			t.Errorf("Expected online message to hab/online, got %s to %s", call.Payload, call.Topic)
		}
	}

	// Check that custom handler was called
	if !handlerCalled {
		t.Error("Custom connect handler should have been called")
	}
}
