package main

import (
	"time"

	. "github.com/elijahnyp/home_controller/util"
)

// MetricProviders builds the state-provider callbacks the OTel observable gauges
// use to read current room/channel/websocket/MQTT state. All reads go through
// the synchronized accessors in state_sync.go.
func MetricProviders() StateProviders {
	return StateProviders{
		RoomStates: func() []RoomMetricState {
			now := time.Now().Unix()
			statuses := CurrentModel().SnapshotRoomStatuses()
			var out []RoomMetricState
			for _, room := range CurrentModel().Rooms {
				occupied, _ := GetOccupancyState(room.Name)
				motion := false
				for _, topic := range room.Motion_topics {
					if GetMotionState(topic) {
						motion = true
						break
					}
				}
				since := int64(0)
				if st, ok := statuses[room.Name]; ok {
					since = now - st.GetLastOccupied()
				}
				out = append(out, RoomMetricState{
					Name:                 room.Name,
					Occupied:             occupied,
					Motion:               motion,
					SecondsSinceOccupied: since,
				})
			}
			return out
		},
		ChannelDepths: func() map[string]int {
			return map[string]int{
				"image":   len(image_channel),
				"motion":  len(motion_channel),
				"door":    len(door_channel),
				"results": len(results_channel),
			}
		},
		WSClients: func() int {
			if wsHub == nil {
				return 0
			}
			return int(wsHub.ClientCount())
		},
		MQTTConnected: func() bool {
			return Client != nil && Client.IsConnected()
		},
	}
}
