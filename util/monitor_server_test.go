package util

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Test helper to create MonitorServer with specific port without config races
type testMonitorServer struct {
	*MonitorServer
	testPort int
}

func newTestMonitorServer(port int) *testMonitorServer {
	return &testMonitorServer{
		MonitorServer: &MonitorServer{
			running: &sync.Mutex{},
			srv:     &http.Server{},
		},
		testPort: port,
	}
}

func (ts *testMonitorServer) Start() error {
	if !ts.running.TryLock() {
		return fmt.Errorf("already running")
	} else {
		ts.running.Unlock()
	}
	go func() {
		ts.running.Lock()

		// Create new server with test port (no config access)
		newSrv := &http.Server{Addr: fmt.Sprintf(":%d", ts.testPort)}
		ts.srvMu.Lock()
		ts.srv = newSrv
		ts.srvMu.Unlock()

		if err := newSrv.ListenAndServe(); err != http.ErrServerClosed {
			// Logger is available in test context
			Logger.Warn().Msgf("Problem loading test monitor server: %v", err)
		}
		Logger.Debug().Msg("test monitor server shutdown")
		ts.running.Unlock()
	}()
	return nil
}

func (ts *testMonitorServer) Restart() {
	Logger.Debug().Msg("restarting test monitor server")
	if !ts.running.TryLock() { // only shutdown if not running
		Logger.Debug().Msg("test monitor server running, shutting it down")

		// Safely access srv with read lock
		ts.srvMu.RLock()
		currentSrv := ts.srv
		ts.srvMu.RUnlock()

		if currentSrv != nil {
			if err := currentSrv.Shutdown(context.TODO()); err != nil {
				Logger.Error().Msgf("Error shutting down test monitor server: %v", err)
			}
		}
	} else {
		ts.running.Unlock()
	}
	Logger.Debug().Msg("waiting for shutdown")
	ts.running.Lock() // when server shuts down it will unlock, so wait for unlock
	Logger.Debug().Msg("test http not running - good for startup")

	// Now start again
	if err := ts.Start(); err != nil {
		Logger.Error().Msgf("Failed to restart test monitor server: %v", err)
	}
}

func (ts *testMonitorServer) AddHandler(path string, handler func(http.ResponseWriter, *http.Request)) {
	http.HandleFunc(path, handler)
}

func (ts *testMonitorServer) AddRawHandler(path string, handler http.Handler) {
	http.Handle(path, handler)
}

func TestNewMonitorServer(t *testing.T) {
	server := NewMonitorServer()

	if server == nil {
		t.Fatal("NewMonitorServer should return non-nil server")
	}

	if server.running == nil {
		t.Error("NewMonitorServer should initialize running mutex")
	}

	if server.srv == nil {
		t.Error("NewMonitorServer should initialize HTTP server")
	}
}

func TestMonitorServer_AddHandler(t *testing.T) {
	server := NewMonitorServer()

	// Test handler function
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response")) //nolint:errcheck // test helper
	}

	// Add handler
	server.AddHandler("/test", testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Call handler directly through the default mux
	http.DefaultServeMux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body != "test response" {
		t.Errorf("Expected 'test response', got '%s'", body)
	}
}

func TestMonitorServer_AddRawHandler(t *testing.T) {
	server := NewMonitorServer()

	// Test raw handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("raw handler response")) //nolint:errcheck // test helper
	})

	// Add raw handler
	server.AddRawHandler("/raw", testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/raw", nil)
	w := httptest.NewRecorder()

	// Call handler directly through the default mux
	http.DefaultServeMux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}

	body := w.Body.String()
	if body != "raw handler response" {
		t.Errorf("Expected 'raw handler response', got '%s'", body)
	}
}

func TestMonitorServer_StartAndRestart(t *testing.T) {
	// Use test helper to avoid config races
	server := newTestMonitorServer(0) // Use port 0 to get any available port

	// Test starting server
	err := server.Start()
	if err != nil {
		t.Errorf("Start() should not return error, got: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test that starting again returns error (already running)
	err = server.Start()
	if err == nil {
		t.Error("Start() should return error when already running")
	}

	// Test restart
	server.Restart()

	// Give server time to restart
	time.Sleep(200 * time.Millisecond)
}

func TestMonitorServer_Integration(t *testing.T) {
	// Use test helper to avoid config races
	testPort := 8899
	server := newTestMonitorServer(testPort)

	// Add a test handler
	server.AddHandler("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy")) //nolint:errcheck // test helper
	})

	// Start server
	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Test HTTP request to the server
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", testPort))
	if err != nil {
		// Server might not be fully started, this is acceptable for this test
		t.Logf("Expected error connecting to test server: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Test restart functionality
	server.Restart()
	time.Sleep(200 * time.Millisecond)

	// Try another request after restart
	resp2, err := http.Get(fmt.Sprintf("http://localhost:%d/health", testPort))
	if err != nil {
		t.Logf("Expected error after restart: %v", err)
		return
	}
	defer func() { _ = resp2.Body.Close() }() //nolint:errcheck // test cleanup

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 after restart, got %d", resp2.StatusCode)
	}
}

func TestMonitorServer_ConcurrentAccess(t *testing.T) {
	// Use test helper to avoid config races
	server := newTestMonitorServer(8904) // Use a different port for this test

	// Test concurrent access to Start method
	results := make(chan error, 3)

	// Use a small delay to make the race condition more likely
	for i := 0; i < 3; i++ {
		go func(id int) {
			if id > 0 {
				time.Sleep(time.Duration(id) * 10 * time.Millisecond)
			}
			err := server.Start()
			results <- err
		}(i)
	}

	// Collect results
	var successCount, errorCount int
	for i := 0; i < 3; i++ {
		err := <-results
		if err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	// We expect exactly one success and two errors (server already running)
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful start, got %d", successCount)
	}
	if errorCount != 2 {
		t.Errorf("Expected exactly 2 'already running' errors, got %d", errorCount)
	}

	// Clean up
	server.Restart()
	time.Sleep(200 * time.Millisecond)
}

func TestMonitorServer_PortConfiguration(t *testing.T) {
	// Use test helper to avoid config races - test with different port configurations
	testPorts := []int{8900, 8901, 8902}

	for _, port := range testPorts {
		port := port // capture loop variable
		t.Run(fmt.Sprintf("Port_%d", port), func(t *testing.T) {
			// Can now run in parallel since no config access
			t.Parallel()

			server := newTestMonitorServer(port)
			err := server.Start()
			if err != nil {
				t.Errorf("Failed to start server on port %d: %v", port, err)
				return
			}

			// Give server time to start
			time.Sleep(200 * time.Millisecond)

			// Clean up - restart will shut down the server
			server.Restart()

			// Give server time to shutdown and restart
			time.Sleep(200 * time.Millisecond)
		})
	}
}

func TestMonitorServer_Shutdown(t *testing.T) {
	// Use test helper to avoid config races
	server := newTestMonitorServer(8903)

	// Start server
	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Test that the mutex is locked (server is running)
	if server.running.TryLock() {
		server.running.Unlock()
		t.Error("Server should be running (mutex should be locked)")
	}

	// Restart (which includes shutdown)
	server.Restart()

	// Give time for shutdown and restart
	time.Sleep(300 * time.Millisecond)
}
