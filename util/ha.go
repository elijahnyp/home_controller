package util

import (
	"encoding/json"
	"fmt"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type HAAvdvertisementAvailability struct {
	Topic               string `json:"topic"`                 // : "home-assistant/window/availability"
	PayloadAvailable    string `json:"payload_available"`     // : "online"
	PayloadNotAvailable string `json:"payload_not_available"` // : "offline"
}

type HADeviceSpec struct {
	Name        string   `json:"name"` // : "Window Contact Sensor"
	Identifiers []string `json:"ids"`  // : ["window_contact_sensor"]
}

type HAAdvertisement struct { //nolint:govet // struct layout optimized for JSON field order
	HAAvdvertisementAvailability []HAAvdvertisementAvailability `json:"availability"`
	Device                       HADeviceSpec                   `json:"device"`      // Device info
	UniqueID                     string                         `json:"uniq_id"`     // "window_contact_sensor_1"
	Name                         string                         `json:"name"`        // : "Window Contact Sensor"
	StateTopic                   string                         `json:"state_topic"` // : "home-assistant/window/contact"
	PayloadOn                    string                         `json:"payload_on"`  // : "ON"
	PayloadOff                   string                         `json:"payload_off"`
	DeviceClass                  string                         `json:"device_class"` // : "occupancy"
	Platform                     string                         `json:"platform"`     // "binary-sensor"
	Qos                          int                            `json:"qos"`
}

func (ha HAAdvertisement) ToJson() string {
	data, err := json.Marshal(ha)
	if err != nil {
		Logger.Error().Msgf("Error marshalling HAAdvertisement: %v", err)
		return ""
	}
	return string(data)
}

func ConstructHAAdvertisement(name, stateTopic string) HAAdvertisement {
	return HAAdvertisement{
		Name:       name,
		StateTopic: stateTopic,
		PayloadOn:  "true",
		PayloadOff: "false",
		HAAvdvertisementAvailability: []HAAvdvertisementAvailability{
			{
				Topic:               "hab/online",
				PayloadAvailable:    "online",
				PayloadNotAvailable: "offline",
			},
		},
		Qos: 0,
		// IDs: []string{"occupancy_sensor_" + name},
		UniqueID:    "occupancy_sensor-" + name,
		DeviceClass: "occupancy",
		// ValueTemplate: valueTemplate,
		Platform: "binary_sensor",
		Device: HADeviceSpec{
			Name:        "occupancy_controller",
			Identifiers: []string{"occupancy_controller"},
		},
	}
}

func AdvertiseHA(r []Room, client MQTT.Client) {
	for _, room := range r {
		if room.Occupancy_topic != "" {
			ha := ConstructHAAdvertisement(room.Name, room.Occupancy_topic)
			if token := client.Publish("homeassistant/binary_sensor/"+room.Name+"/occupancy/config", 0, false, ha.ToJson()); token.Wait() && token.Error() != nil {
				Logger.Panic().Msgf("Error Publishing: %v", fmt.Errorf("%v", token.Error()))
			}
		}
	}
}
