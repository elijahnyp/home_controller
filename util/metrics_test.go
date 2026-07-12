package util

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInitMetricsAndExposition(t *testing.T) {
	handler, err := InitMetrics(StateProviders{
		RoomStates: func() []RoomMetricState {
			return []RoomMetricState{
				{Name: "office", Occupied: true, Motion: false, SecondsSinceOccupied: 5},
			}
		},
		ChannelDepths: func() map[string]int { return map[string]int{"image": 1} },
		WSClients:     func() int { return 2 },
		MQTTConnected: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("InitMetrics: %v", err)
	}

	// Exercise a few recording paths.
	RecordObject("office", "person", 0.9)
	RecordDetection("yolo11", "ok", 12*time.Millisecond)
	RecordOccupancyTransition("office", "occupied")
	RecordMessageReceived("pic")
	RecordPublish("ok", 3*time.Millisecond)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 from /metrics, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"objects_detected_total",
		"detection_requests_total",
		"occupancy_transitions_total",
		"room_occupied",
		"channel_queue_depth",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}
