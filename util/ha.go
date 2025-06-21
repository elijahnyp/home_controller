package util

import(
	"encoding/json"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"fmt"
)

type HAAvdvertisementAvailability struct {
	Topic string `json:"topic"` //: "home-assistant/window/availability"
	PayloadAvailable string  `json:"payload_available"` //: "online"
	PayloadNotAvailable string  `json:"payload_not_available"` //: "offline"
}

type HADeviceSpec struct {
	Name string `json:"name"` //: "Window Contact Sensor"
	Identifiers []string `json:"ids"` //: ["window_contact_sensor"]
}

type HAAdvertisement struct {
      Name string `json:"name"` //: "Window Contact Sensor"
      StateTopic string `json:"state_topic"` //: "home-assistant/window/contact"
      PayloadOn string `json:"payload_on"` //: "ON"
	  PayloadOff string `json:"payload_off"`
      HAAvdvertisementAvailability []HAAvdvertisementAvailability `json:"availability"`
      Qos int`json:"qos"`
      DeviceClass string `json:"device_class"` //: "occupancy"
    //   ValueTemplate string `json:"value_template"` //: "{{ value_json.state }}"
	  Platform string `json:"platform"` // "binary-sensor"
	//   IDs []string `json:ids,omitempty` // "window_contact_sensor"
	  UniqueID string `json:"uniq_id"` // "window_contact_sensor_1"
	  Device HADeviceSpec `json:"device"` //: {"name": "Window Contact Sensor", "ids": ["window_contact_sensor"]}
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
		Name: name,
		StateTopic: stateTopic,
		PayloadOn: "true",
		PayloadOff: "false",
		HAAvdvertisementAvailability: []HAAvdvertisementAvailability{
			{
				Topic: "hab/online",
				PayloadAvailable: "online",
				PayloadNotAvailable: "offline",
			},
		},
		Qos: 0,
		// IDs: []string{"occupancy_sensor_" + name},
		UniqueID: "occupancy_sensor-" + name,
		DeviceClass: "occupancy",
		// ValueTemplate: valueTemplate,
		Platform: "binary_sensor",
		Device: HADeviceSpec{
			Name: "occupancy_controller",
			Identifiers: []string{"occupancy_controller"},
		},
	}
}

func AdvertiseHA(r []Room, Client MQTT.Client) {
	for _, room := range r{
		if room.Occupancy_topic != "" {
			ha := ConstructHAAdvertisement(room.Name, room.Occupancy_topic)
			if token := Client.Publish("homeassistant/binary_sensor/" + room.Name + "/occupancy/config", 0, false, ha.ToJson()); token.Wait() && token.Error() != nil {
				Logger.Panic().Msgf("Error Publishing: %v", fmt.Errorf("%v", token.Error()))
			}
		}
	}
}