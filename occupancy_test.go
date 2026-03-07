package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	tritonpb "github.com/elijahnyp/home_controller/triton/generated"
	. "github.com/elijahnyp/home_controller/util"
	"google.golang.org/grpc"
)

// mockTritonServer implements just ModelInfer, returning a single person detection.
type mockTritonServer struct {
	tritonpb.UnimplementedGRPCInferenceServiceServer
}

func (s *mockTritonServer) ModelInfer(_ context.Context, _ *tritonpb.ModelInferRequest) (*tritonpb.ModelInferResponse, error) {
	// Build a minimal YOLO11 output tensor [1, 84, 8400] with one person.
	const numClasses = 80
	const numAnchors = 8400
	numRows := 4 + numClasses
	total := numRows * numAnchors

	floats := make([]float32, total)
	// Anchor 0: cx=320, cy=320, w=100, h=100, class[0]=0.9
	floats[0*numAnchors+0] = 320 // cx
	floats[1*numAnchors+0] = 320 // cy
	floats[2*numAnchors+0] = 100 // w
	floats[3*numAnchors+0] = 100 // h
	floats[4*numAnchors+0] = 0.9 // person confidence

	rawBytes := make([]byte, len(floats)*4)
	for i, v := range floats {
		binary.LittleEndian.PutUint32(rawBytes[i*4:], math.Float32bits(v))
	}

	return &tritonpb.ModelInferResponse{
		ModelName: "yolo11",
		Outputs: []*tritonpb.ModelInferResponse_InferOutputTensor{
			{
				Name:     "output0",
				Datatype: "FP32",
				Shape:    []int64{1, int64(numRows), int64(numAnchors)},
			},
		},
		RawOutputContents: [][]byte{rawBytes},
	}, nil
}

func startMockTritonServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock triton listener: %v", err)
	}
	const maxMsgSize = 32 * 1024 * 1024 // 32 MB – needed for 640×640×3 FP32 input
	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
	)
	tritonpb.RegisterGRPCInferenceServiceServer(srv, &mockTritonServer{})
	go func() {
		_ = srv.Serve(lis) //nolint:errcheck // Serve returns nil on Stop(); error is not actionable in a test helper
	}()
	return lis.Addr().String(), func() { srv.Stop() }
}

func TestProcessImage(t *testing.T) {
	// Start mock Triton gRPC server.
	addr, stop := startMockTritonServer(t)
	defer stop()

	// Point client at the mock server.
	Config.Set("triton_url", addr)
	Config.Set("triton_model", "yolo11")
	Config.Set("triton_input_width", 640)
	Config.Set("triton_input_height", 640)
	Config.Set("triton_input_name", "images")
	Config.Set("triton_output_name", "output0")
	Config.Set("min_confidence", 0.5)
	Config.Set("triton_iou_threshold", 0.45)
	Config.Set("frequency", 1)

	if err := InitTritonClient(); err != nil {
		t.Fatalf("InitTritonClient: %v", err)
	}

	// Build a small test JPEG.
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for x := 0; x < 100; x++ {
		for y := 0; y < 100; y++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil) //nolint:errcheck // test helper
	jpegData := buf.Bytes()

	// Verify DetectObjects works directly before testing via channel.
	dets, err := DetectObjects(jpegData)
	if err != nil {
		t.Fatalf("DetectObjects: %v", err)
	}
	t.Logf("DetectObjects returned %d detection(s): %+v", len(dets), dets)

	testItem := MQTT_Item{
		Room:  "test_room",
		Data:  jpegData,
		Topic: "test/topic",
		Type:  PIC,
	}

	results_channel = make(chan MQTT_Item, 10)
	go ProcessImage(testItem)

	select {
	case result := <-results_channel:
		if result.Analysis_result != OCCUPIED {
			t.Errorf("Expected OCCUPIED, got %d", result.Analysis_result)
		}
		if result.Room != "test_room" {
			t.Errorf("Expected room 'test_room', got %s", result.Room)
		}
	case <-time.After(10 * time.Second):
		t.Error("Timeout waiting for image processing result")
	}
}

func TestMarkupImage(t *testing.T) {
	// Create test image
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for x := 0; x < 300; x++ {
		for y := 0; y < 300; y++ {
			img.Set(x, y, color.RGBA{100, 100, 100, 255})
		}
	}

	// Create markup specs
	specs := []MarkupSpec{
		{
			min:        point{x: 50, y: 50},
			max:        point{x: 150, y: 150},
			label:      "person",
			confidence: 0.85,
		},
	}

	// Apply markup
	markedImg := MarkupImage(img, specs)

	// Verify markup was applied (check for red pixels at corners)
	bounds := markedImg.Bounds()
	if bounds.Max.X != 300 || bounds.Max.Y != 300 {
		t.Errorf("Expected bounds 300x300, got %dx%d", bounds.Max.X, bounds.Max.Y)
	}

	// Check if red markup exists at expected location
	r, _, _, _ := markedImg.At(50, 50).RGBA()
	if r < 50000 { // Should be red
		t.Error("Expected red markup at position (50, 50)")
	}
}

func TestMotionManagerRoutine(t *testing.T) {
	// Initialize channels
	motion_channel = make(chan MQTT_Item, 10)
	results_channel = make(chan MQTT_Item, 10)

	// Start motion manager in goroutine
	go MotionManagerRoutine()

	tests := []struct {
		name     string
		data     string
		expected int
	}{
		{"Motion start integer", "1", MOTION_START},
		{"Motion stop integer", "0", MOTION_STOP},
		{"Motion start string", "ON", MOTION_START},
		{"Motion stop string", "OFF", MOTION_STOP},
		{"Door open", "OPEN", MOTION_STOP},
		{"Door closed", "CLOSED", MOTION_START},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Send test motion data
			testItem := MQTT_Item{
				Room:  "test_room",
				Data:  []byte(tt.data),
				Topic: "test/motion",
				Type:  MOTION,
			}

			motion_channel <- testItem

			// Wait for result
			select {
			case result := <-results_channel:
				if result.Analysis_result != tt.expected {
					t.Errorf("Expected result %d, got %d", tt.expected, result.Analysis_result)
				}
			case <-time.After(1 * time.Second):
				t.Error("Timeout waiting for motion processing result")
			}
		})
	}
}

func TestHttpImage(t *testing.T) {
	// Setup cache with test data
	testImg := image.NewRGBA(image.Rect(0, 0, 100, 100))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, testImg, nil) //nolint:errcheck // test helper

	cache = make(map[string]ImageCacheItem)
	cache["test/topic"] = ImageCacheItem{
		im: buf.Bytes(),
		results: ai_results{
			Success: true,
			Predictions: []ai_result{
				{
					Confidence: 0.9,
					Label:      "person",
					X_min:      10,
					Y_min:      10,
					X_max:      50,
					Y_max:      50,
				},
			},
		},
	}

	tests := []struct {
		name           string
		method         string
		id             string
		expectedType   string
		expectedStatus int
	}{
		{"Valid GET request", "GET", "test/topic", "image/jpeg", 200},
		{"Invalid ID", "GET", "nonexistent", "", 404},
		{"Invalid method", "POST", "test/topic", "", 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/?id="+tt.id, nil)
			w := httptest.NewRecorder()

			HttpImage(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedType != "" {
				contentType := w.Header().Get("Content-Type")
				if contentType != tt.expectedType {
					t.Errorf("Expected content type %s, got %s", tt.expectedType, contentType)
				}
			}
		})
	}
}

func TestStatusOverview(t *testing.T) {
	// Setup model with test data
	model = Model{
		Rooms: []Room{
			{
				Name:             "test_room",
				Occupancy_topic:  "hab/test_room/occupancy",
				Occupancy_period: 120,
			},
		},
	}

	// Initialize model status
	modelStatus := &ModelStatus{
		Room_status: make(map[string]RoomStatus),
	}
	testRoomStatus := RoomStatus{}
	testRoomStatus.Occupied() // This will set the internal fields properly
	testRoomStatus.Motion(true)
	modelStatus.Room_status["test_room"] = testRoomStatus

	// We need to access this through the model's method
	tempModel := &Model{}
	_ = tempModel.BuildModel() //nolint:errcheck // test helper initialization
	tempModel.UpdateRoomStatus("test_room", testRoomStatus)

	req := httptest.NewRequest("GET", "/room_status", nil)
	w := httptest.NewRecorder()

	StatusOverview(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("Expected content type text/html, got %s", contentType)
	}

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("test_room")) {
		t.Error("Expected response to contain 'test_room'")
	}
}

func TestModelApi(t *testing.T) {
	// Setup model with test data
	model = Model{
		Rooms: []Room{
			{
				Name:       "test_room",
				Pic_topics: []string{"test/camera1", "test/camera2"},
			},
		},
	}

	// Setup cache
	cache = make(map[string]ImageCacheItem)
	cache["test/camera1"] = ImageCacheItem{
		results: ai_results{
			Success: true,
			Predictions: []ai_result{
				{Label: "person", Confidence: 0.9},
			},
		},
	}

	tests := []struct {
		name           string
		method         string
		room           string
		expectedStatus int
	}{
		{"Valid room request", "GET", "test_room", 200},
		{"All rooms request", "GET", "", 200},
		{"Invalid room", "GET", "nonexistent", 404},
		{"Invalid method", "POST", "test_room", 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/model"
			if tt.room != "" {
				url += "?room=" + tt.room
			}

			req := httptest.NewRequest(tt.method, url, nil)
			w := httptest.NewRecorder()

			ModelApi(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedStatus == 200 {
				contentType := w.Header().Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("Expected content type application/json, got %s", contentType)
				}

				var response map[string]modelapiresponseitem
				err := json.Unmarshal(w.Body.Bytes(), &response)
				if err != nil {
					t.Errorf("Failed to unmarshal response: %v", err)
				}
			}
		})
	}
}

// Mock MQTT client for testing
type mockMQTTClient struct{}

func (m *mockMQTTClient) IsConnected() bool       { return true }
func (m *mockMQTTClient) IsConnectionOpen() bool  { return true }
func (m *mockMQTTClient) Connect() MQTT.Token     { return &mockToken{} }
func (m *mockMQTTClient) Disconnect(quiesce uint) {}
func (m *mockMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) MQTT.Token {
	return &mockToken{}
}
func (m *mockMQTTClient) Subscribe(topic string, qos byte, callback MQTT.MessageHandler) MQTT.Token {
	return &mockToken{}
}
func (m *mockMQTTClient) SubscribeMultiple(filters map[string]byte, callback MQTT.MessageHandler) MQTT.Token {
	return &mockToken{}
}
func (m *mockMQTTClient) Unsubscribe(topics ...string) MQTT.Token             { return &mockToken{} }
func (m *mockMQTTClient) AddRoute(topic string, callback MQTT.MessageHandler) {}
func (m *mockMQTTClient) OptionsReader() MQTT.ClientOptionsReader             { return MQTT.ClientOptionsReader{} }

type mockToken struct{}

func (m *mockToken) Wait() bool                     { return true }
func (m *mockToken) WaitTimeout(time.Duration) bool { return true }
func (m *mockToken) Done() <-chan struct{}          { return make(<-chan struct{}) }
func (m *mockToken) Error() error                   { return nil }

func TestOnlinePinger(t *testing.T) {
	// Use mock client
	Client = &mockMQTTClient{}

	// Start pinger in goroutine and stop after short time
	done := make(chan bool)
	go func() {
		time.Sleep(100 * time.Millisecond)
		done <- true
	}()

	go func() {
		for {
			select {
			case <-done:
				return
			default:
				OnlinePinger()
			}
		}
	}()

	// Just verify it doesn't panic
	select {
	case <-done:
		// Test passed
	case <-time.After(200 * time.Millisecond):
		t.Error("OnlinePinger test timeout")
	}
}
