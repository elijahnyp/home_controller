package util

import (
	"crypto/rand"
	"fmt"
	"reflect"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

const ENV_PREFIX = ""

var Config = viper.New()

var config_listeners []func()

func RegisterNewConfigListener(new_listener func()) {
	for _, listener := range config_listeners {
		if reflect.ValueOf(new_listener).Pointer() == reflect.ValueOf(listener).Pointer() {
			Logger.Warn().Msg("config listener already registered")
			return
		}
	}
	config_listeners = append(config_listeners, new_listener)
}

func OnNewConfig() {
	for _, listener := range config_listeners {
		listener()
	}
}

func GetRandString(n int) string {
	// using crypto/rand for better security
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		randBytes := make([]byte, 1)
		if _, err := rand.Read(randBytes); err != nil {
			// fallback to a simple approach if crypto/rand fails
			b[i] = letterBytes[i%len(letterBytes)]
		} else {
			b[i] = letterBytes[int(randBytes[0])%len(letterBytes)]
		}
	}
	return string(b)
}

func SetupConfig() {
	Config.SetEnvPrefix(ENV_PREFIX)
	// set defaults
	Config.SetDefault("Broker_URI", "tcp://mqtt")
	Config.SetDefault("Cleansess", false)
	Config.SetDefault("Id", GetRandString(10))
	Config.SetDefault("Username", "")
	Config.SetDefault("Password", "")
	Config.SetDefault("Frequency", 30)
	Config.SetDefault("Occupancy_period", 150)

	// config file
	Config.SetConfigName("home_controller")
	Config.AddConfigPath("/")
	Config.AddConfigPath("./")
	Config.AddConfigPath("./config")
	Config.AddConfigPath("/etc")
	Config.AddConfigPath("/home_controller")
	Config.AddConfigPath("/home_controller/config")

	err := Config.ReadInConfig()
	if err != nil {
		Logger.Error().Msgf("unable to read config file: %v", fmt.Errorf("%v", err))
	}

	// environment variables
	Config.AutomaticEnv()

	// flags

	// watch for changes
	Config.WatchConfig()
	Config.OnConfigChange(func(e fsnotify.Event) {
		Logger.Info().Msgf("Config file changed: %v", e.Name)
		Logger.Debug().Msgf("Config Additional Info: %v", e.String())
		OnNewConfig()
	})

}
