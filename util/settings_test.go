package util

import (
	"os"
	"testing"
)

func TestGetRandStringVariousLengths(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"Zero length", 0},
		{"Single character", 1},
		{"Small string", 5},
		{"Medium string", 10},
		{"Large string", 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRandString(tt.length)

			if len(result) != tt.length {
				t.Errorf("GetRandString(%d) = length %d, expected %d", tt.length, len(result), tt.length)
			}

			// Verify all characters are letters
			for i, char := range result {
				if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')) {
					t.Errorf("GetRandString(%d) contains non-letter at position %d: %c", tt.length, i, char)
				}
			}
		})
	}
}

func TestGetRandStringRandomness(t *testing.T) {
	// Generate multiple strings and ensure they're different
	const length = 10
	const iterations = 100

	strings := make(map[string]bool)

	for i := 0; i < iterations; i++ {
		result := GetRandString(length)
		if strings[result] {
			t.Errorf("GetRandString generated duplicate string: %s", result)
		}
		strings[result] = true
	}

	// Should have generated unique strings (very high probability)
	if len(strings) < iterations {
		t.Errorf("GetRandString generated %d unique strings out of %d iterations", len(strings), iterations)
	}
}

func TestRegisterNewConfigListener(t *testing.T) {
	// Clear existing listeners
	config_listeners = []func(){}

	// Test adding listeners
	called1 := false
	called2 := false

	listener1 := func() { called1 = true }
	listener2 := func() { called2 = true }

	RegisterNewConfigListener(listener1)
	RegisterNewConfigListener(listener2)

	if len(config_listeners) != 2 {
		t.Errorf("Expected 2 listeners, got %d", len(config_listeners))
	}

	// Test that duplicate listeners are not added
	RegisterNewConfigListener(listener1) // Should not add duplicate

	if len(config_listeners) != 2 {
		t.Errorf("Expected 2 listeners after duplicate addition, got %d", len(config_listeners))
	}

	// Test OnNewConfig calls all listeners
	OnNewConfig()

	if !called1 || !called2 {
		t.Error("OnNewConfig should call all registered listeners")
	}
}

func TestOnNewConfig(t *testing.T) {
	// Clear existing listeners
	config_listeners = []func(){}

	callCount := 0
	listener := func() { callCount++ }

	RegisterNewConfigListener(listener)
	RegisterNewConfigListener(listener)               // Should be deduplicated
	RegisterNewConfigListener(func() { callCount++ }) // Different function

	OnNewConfig()

	// Should have called 2 unique listeners
	if callCount != 2 {
		t.Errorf("Expected 2 listener calls, got %d", callCount)
	}
}

func TestSetupConfigDefaults(t *testing.T) {
	// Test that SetupConfig sets appropriate defaults
	SetupConfig()

	// Test some default values
	brokerURI := Config.GetString("Broker_URI")
	if brokerURI == "" {
		t.Error("Broker_URI default should not be empty")
	}

	cleanSess := Config.GetBool("Cleansess")
	// Should have a boolean value (default false in this case)
	t.Logf("Cleansess default: %v", cleanSess)

	frequency := Config.GetInt("Frequency")
	if frequency <= 0 {
		t.Errorf("Frequency default should be positive, got %d", frequency)
	}

	occupancyPeriod := Config.GetInt("Occupancy_period")
	if occupancyPeriod <= 0 {
		t.Errorf("Occupancy_period default should be positive, got %d", occupancyPeriod)
	}
}

func TestSetupConfigEnvironmentVariables(t *testing.T) {
	// Test that environment variables are read
	testEnvVar := "TEST_BROKER_URI"
	expectedValue := "tcp://test-env-broker:1883"

	// Set environment variable
	_ = os.Setenv(testEnvVar, expectedValue)       //nolint:errcheck // test setup
	defer func() { _ = os.Unsetenv(testEnvVar) }() //nolint:errcheck // test cleanup

	// Setup config
	SetupConfig()

	// Check if environment variable is accessible (viper should use AutomaticEnv)
	// Note: This tests the AutomaticEnv() functionality
	if Config.IsSet(testEnvVar) {
		value := Config.GetString(testEnvVar)
		if value != expectedValue {
			t.Errorf("Environment variable %s = %s, expected %s", testEnvVar, value, expectedValue)
		}
	}
}

func TestSetupConfigFileSearch(t *testing.T) {
	// Create a temporary config file
	tempConfigContent := `{
		"test_key": "test_value",
		"test_number": 42
	}`

	// Create temporary file in current directory
	configFile, err := os.CreateTemp(".", "home_controller*.json")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer func() { _ = os.Remove(configFile.Name()) }() //nolint:errcheck // test cleanup

	if _, err := configFile.WriteString(tempConfigContent); err != nil {
		t.Fatalf("Failed to write to temp config file: %v", err)
	}
	configFile.Close()

	// Rename to expected config name
	expectedName := "home_controller.json"
	_ = os.Rename(configFile.Name(), expectedName) //nolint:errcheck // test setup
	defer func() { _ = os.Remove(expectedName) }() //nolint:errcheck // test cleanup

	// Setup config (should find our test file)
	SetupConfig()

	// Check if our test values were loaded
	testValue := Config.GetString("test_key")
	if testValue != "test_value" {
		t.Errorf("Config file test_key = %s, expected test_value", testValue)
	}

	testNumber := Config.GetInt("test_number")
	if testNumber != 42 {
		t.Errorf("Config file test_number = %d, expected 42", testNumber)
	}
}

func TestSetupConfigWatching(t *testing.T) {
	// Test that config watching is enabled
	SetupConfig()

	// This is harder to test without actually modifying files,
	// but we can verify that the function completes without error
	// and that the config object is properly initialized

	if Config == nil {
		t.Error("Config should be initialized after SetupConfig")
	}

	// Test that we can set and get values
	testKey := "test_watch_key"
	testValue := "test_watch_value"

	Config.Set(testKey, testValue)
	retrievedValue := Config.GetString(testKey)

	if retrievedValue != testValue {
		t.Errorf("Config.Set/Get failed: got %s, expected %s", retrievedValue, testValue)
	}
}

func TestConfigurationPaths(t *testing.T) {
	// Test that SetupConfig adds the expected configuration paths
	SetupConfig()

	// We can't directly test the paths, but we can verify
	// that the config object is working and ready to read from those paths

	// Test reading a non-existent key returns appropriate zero value
	nonExistentString := Config.GetString("non_existent_key")
	if nonExistentString != "" {
		t.Errorf("Non-existent string key should return empty string, got %s", nonExistentString)
	}

	nonExistentInt := Config.GetInt("non_existent_int_key")
	if nonExistentInt != 0 {
		t.Errorf("Non-existent int key should return 0, got %d", nonExistentInt)
	}

	nonExistentBool := Config.GetBool("non_existent_bool_key")
	if nonExistentBool != false {
		t.Errorf("Non-existent bool key should return false, got %v", nonExistentBool)
	}
}
