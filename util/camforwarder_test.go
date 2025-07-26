package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCamForwarder_MakeCamForwarder(t *testing.T) {
	// Setup test configuration
	testConfig := map[string]interface{}{
		"enabled":   true,
		"frequency": int64(5),
		"workers":   int64(2),
		"cameras": []map[string]interface{}{
			{
				"snap_url": "http://test.camera1/snap.jpg",
				"topic":    "test/camera1/image",
			},
			{
				"snap_url": "http://test.camera2/snap.jpg",
				"topic":    "test/camera2/image",
			},
		},
	}

	Config.Set("cam_forwarder", testConfig)

	cf := &CamForwarder{}
	cf.MakeCamForwarder()

	// Test that configuration was loaded
	if !cf.Enabled {
		t.Error("CamForwarder should be enabled")
	}

	if cf.Frequency != 5 {
		t.Errorf("Frequency = %d, expected 5", cf.Frequency)
	}

	if cf.Workers != 2 {
		t.Errorf("Workers = %d, expected 2", cf.Workers)
	}

	if len(cf.Cameras) != 2 {
		t.Errorf("Expected 2 cameras, got %d", len(cf.Cameras))
	}

	// Test camera configuration
	if cf.Cameras[0].Url != "http://test.camera1/snap.jpg" {
		t.Errorf("Camera 0 URL = %s, expected http://test.camera1/snap.jpg", cf.Cameras[0].Url)
	}

	if cf.Cameras[0].Topic != "test/camera1/image" {
		t.Errorf("Camera 0 Topic = %s, expected test/camera1/image", cf.Cameras[0].Topic)
	}
}

func TestCamForwarder_Start(t *testing.T) {
	// Setup configuration
	testConfig := map[string]interface{}{
		"enabled":   true,
		"frequency": int64(1), // 1 second for faster testing
		"workers":   int64(1),
		"cameras": []map[string]interface{}{
			{
				"snap_url": "http://test.camera/snap.jpg",
				"topic":    "test/camera/image",
			},
		},
	}

	Config.Set("cam_forwarder", testConfig)

	cf := &CamForwarder{}
	cf.MakeCamForwarder()

	// Initialize a test queue to capture jobs
	queue = make(chan CamForwarderCamera, 10)

	// Start the forwarder
	cf.Start()

	// Wait for at least one job to be queued
	select {
	case job := <-queue:
		if job.Url != "http://test.camera/snap.jpg" {
			t.Errorf("Job URL = %s, expected http://test.camera/snap.jpg", job.Url)
		}
		if job.Topic != "test/camera/image" {
			t.Errorf("Job Topic = %s, expected test/camera/image", job.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Error("No job received within timeout")
	}

	// Stop the ticker to clean up
	if ticker != nil {
		ticker.Stop()
	}
}

func TestProcess_job_Success(t *testing.T) {
	// Create mock HTTP server that serves a test image
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		// Return mock JPEG data
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake jpeg data")) //nolint:errcheck // test helper
	}))
	defer mockServer.Close()

	// Setup mock MQTT client
	mockClient := &MockMQTTClient{}
	Client = mockClient

	// Create test job
	job := CamForwarderCamera{
		Url:   mockServer.URL,
		Topic: "test/camera/image",
	}

	// Process the job
	process_job(job)

	// Verify MQTT publish was called
	if len(mockClient.publishCalls) != 1 {
		t.Errorf("Expected 1 MQTT publish call, got %d", len(mockClient.publishCalls))
	} else {
		call := mockClient.publishCalls[0]
		if call.Topic != "test/camera/image" {
			t.Errorf("Published to topic %s, expected test/camera/image", call.Topic)
		}
		payload, ok := call.Payload.([]byte) //nolint:errcheck // test assertion
		if !ok || string(payload) != "fake jpeg data" {
			t.Errorf("Published payload = %s, expected 'fake jpeg data'", call.Payload)
		}
	}
}

func TestProcess_job_HTTPError(t *testing.T) {
	// Create mock HTTP server that returns error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Server Error")) //nolint:errcheck // test helper
	}))
	defer mockServer.Close()

	// Setup mock MQTT client
	mockClient := &MockMQTTClient{}
	Client = mockClient

	// Create test job
	job := CamForwarderCamera{
		Url:   mockServer.URL,
		Topic: "test/camera/image",
	}

	// Process the job (should handle error gracefully)
	process_job(job)

	// Verify no MQTT publish was called due to error
	if len(mockClient.publishCalls) != 0 {
		t.Errorf("Expected 0 MQTT publish calls due to HTTP error, got %d", len(mockClient.publishCalls))
	}
}

func TestProcess_job_InvalidContentType(t *testing.T) {
	// Create mock HTTP server that returns wrong content type
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not an image")) //nolint:errcheck // test helper
	}))
	defer mockServer.Close()

	// Setup mock MQTT client
	mockClient := &MockMQTTClient{}
	Client = mockClient

	// Create test job
	job := CamForwarderCamera{
		Url:   mockServer.URL,
		Topic: "test/camera/image",
	}

	// Process the job (should handle wrong content type)
	process_job(job)

	// Verify no MQTT publish was called due to wrong content type
	if len(mockClient.publishCalls) != 0 {
		t.Errorf("Expected 0 MQTT publish calls due to wrong content type, got %d", len(mockClient.publishCalls))
	}
}

func TestProcess_job_NetworkError(t *testing.T) {
	// Setup mock MQTT client
	mockClient := &MockMQTTClient{}
	Client = mockClient

	// Create test job with invalid URL
	job := CamForwarderCamera{
		Url:   "http://nonexistent.server/image.jpg",
		Topic: "test/camera/image",
	}

	// Process the job (should handle network error gracefully)
	process_job(job)

	// Verify no MQTT publish was called due to network error
	if len(mockClient.publishCalls) != 0 {
		t.Errorf("Expected 0 MQTT publish calls due to network error, got %d", len(mockClient.publishCalls))
	}
}

func TestCam_worker(t *testing.T) {
	// Create a test queue and start worker
	testQueue := make(chan CamForwarderCamera, 5)

	// Create mock HTTP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test image data")) //nolint:errcheck // test helper
	}))
	defer mockServer.Close()

	// Setup mock MQTT client
	mockClient := &MockMQTTClient{}
	Client = mockClient

	// Start worker in goroutine
	go cam_worker(testQueue)

	// Send test job
	job := CamForwarderCamera{
		Url:   mockServer.URL,
		Topic: "test/worker/image",
	}

	testQueue <- job

	// Give worker time to process
	time.Sleep(100 * time.Millisecond)

	// Verify MQTT publish was called
	if len(mockClient.publishCalls) != 1 {
		t.Errorf("Expected 1 MQTT publish call from worker, got %d", len(mockClient.publishCalls))
	} else {
		call := mockClient.publishCalls[0]
		if call.Topic != "test/worker/image" {
			t.Errorf("Worker published to topic %s, expected test/worker/image", call.Topic)
		}
	}

	// Close queue to stop worker
	close(testQueue)
}

func TestCamForwarderCamera_Structure(t *testing.T) {
	camera := CamForwarderCamera{
		Url:   "http://example.com/camera.jpg",
		Topic: "home/camera/image",
	}

	if camera.Url != "http://example.com/camera.jpg" {
		t.Errorf("Camera URL = %s, expected http://example.com/camera.jpg", camera.Url)
	}

	if camera.Topic != "home/camera/image" {
		t.Errorf("Camera Topic = %s, expected home/camera/image", camera.Topic)
	}
}

func TestCamForwarder_Structure(t *testing.T) {
	cf := CamForwarder{
		Enabled:   true,
		Frequency: 10,
		Workers:   3,
		Cameras: []CamForwarderCamera{
			{Url: "http://cam1.local/snap", Topic: "cam1/image"},
			{Url: "http://cam2.local/snap", Topic: "cam2/image"},
		},
	}

	if !cf.Enabled {
		t.Error("CamForwarder should be enabled")
	}

	if cf.Frequency != 10 {
		t.Errorf("Frequency = %d, expected 10", cf.Frequency)
	}

	if cf.Workers != 3 {
		t.Errorf("Workers = %d, expected 3", cf.Workers)
	}

	if len(cf.Cameras) != 2 {
		t.Errorf("Expected 2 cameras, got %d", len(cf.Cameras))
	}
}
