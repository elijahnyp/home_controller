package util

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/otlptranslator"
	runtimeinst "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// RoomMetricState is a point-in-time snapshot of a room used by the observable
// gauges. The main package builds these from its (synchronized) state.
type RoomMetricState struct {
	Name                 string
	Occupied             bool
	Motion               bool
	SecondsSinceOccupied int64
}

// StateProviders supplies current state to the async observable gauges. Any nil
// field is simply skipped, so callers can provide only what they have.
type StateProviders struct {
	RoomStates    func() []RoomMetricState
	ChannelDepths func() map[string]int
	WSClients     func() int
	MQTTConnected func() bool
}

// instruments holds every synchronous instrument. It is published atomically by
// InitMetrics and loaded (lock-free) by the Record* helpers, so recording is
// race-free even if init races with in-flight goroutines and a no-op until init.
type instruments struct {
	detectionDuration    metric.Float64Histogram
	detectionRequests    metric.Int64Counter
	preprocessDuration   metric.Float64Histogram
	messagesReceived     metric.Int64Counter
	messagesPublished    metric.Int64Counter
	publishDuration      metric.Float64Histogram
	imagesSkipped        metric.Int64Counter
	camFetch             metric.Int64Counter
	camFetchDuration     metric.Float64Histogram
	configReloads        metric.Int64Counter
	objectsDetected      metric.Int64Counter
	objectConfidence     metric.Float64Histogram
	personDetections     metric.Int64Counter
	occupancyTransitions metric.Int64Counter
}

var (
	metricsCtx     = context.Background()
	instrumentsPtr atomic.Pointer[instruments]
)

// InitMetrics wires up an OpenTelemetry meter provider backed by a Prometheus
// exporter, registers all instruments (including Go runtime metrics), and
// returns an http.Handler to serve /metrics.
func InitMetrics(p StateProviders) (http.Handler, error) {
	registry := prometheus.NewRegistry()

	// Names already follow Prometheus conventions (explicit _total/_seconds
	// suffixes), so use NoTranslation to emit them verbatim (no automatic unit
	// or counter suffixing).
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithTranslationStrategy(otlptranslator.NoTranslation),
	)
	if err != nil {
		return nil, err
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	meter := provider.Meter("home_controller")

	if err := registerInstruments(meter); err != nil {
		return nil, err
	}
	if err := registerObservables(meter, p); err != nil {
		return nil, err
	}

	// Go runtime metrics (GC, goroutines, memory) via the OTel contrib package.
	if err := runtimeinst.Start(runtimeinst.WithMeterProvider(provider)); err != nil {
		Logger.Warn().Msgf("failed to start runtime metrics: %v", err)
	}

	Logger.Info().Msg("metrics initialized")
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{}), nil
}

func registerInstruments(meter metric.Meter) error {
	var err error
	ins := &instruments{}
	if ins.detectionDuration, err = meter.Float64Histogram("detection_duration_seconds",
		metric.WithDescription("Triton object-detection latency"), metric.WithUnit("s")); err != nil {
		return err
	}
	if ins.detectionRequests, err = meter.Int64Counter("detection_requests_total",
		metric.WithDescription("Object-detection requests")); err != nil {
		return err
	}
	if ins.preprocessDuration, err = meter.Float64Histogram("image_preprocess_duration_seconds",
		metric.WithDescription("Image letterbox+tensor preprocessing latency"), metric.WithUnit("s")); err != nil {
		return err
	}
	if ins.messagesReceived, err = meter.Int64Counter("mqtt_messages_received_total",
		metric.WithDescription("MQTT messages received by topic type")); err != nil {
		return err
	}
	if ins.messagesPublished, err = meter.Int64Counter("mqtt_messages_published_total",
		metric.WithDescription("MQTT publish attempts by result")); err != nil {
		return err
	}
	if ins.publishDuration, err = meter.Float64Histogram("mqtt_publish_duration_seconds",
		metric.WithDescription("MQTT publish round-trip latency"), metric.WithUnit("s")); err != nil {
		return err
	}
	if ins.imagesSkipped, err = meter.Int64Counter("image_processing_skipped_total",
		metric.WithDescription("Images skipped before inference")); err != nil {
		return err
	}
	if ins.camFetch, err = meter.Int64Counter("camforwarder_fetch_total",
		metric.WithDescription("Camera snapshot fetch attempts by result")); err != nil {
		return err
	}
	if ins.camFetchDuration, err = meter.Float64Histogram("camforwarder_fetch_duration_seconds",
		metric.WithDescription("Camera snapshot fetch latency"), metric.WithUnit("s")); err != nil {
		return err
	}
	if ins.configReloads, err = meter.Int64Counter("config_reloads_total",
		metric.WithDescription("Configuration reloads applied")); err != nil {
		return err
	}
	if ins.objectsDetected, err = meter.Int64Counter("objects_detected_total",
		metric.WithDescription("Objects detected by room and class")); err != nil {
		return err
	}
	if ins.objectConfidence, err = meter.Float64Histogram("object_confidence",
		metric.WithDescription("Detection confidence distribution by room and class")); err != nil {
		return err
	}
	if ins.personDetections, err = meter.Int64Counter("person_detections_total",
		metric.WithDescription("Frames with a person detected, by room")); err != nil {
		return err
	}
	if ins.occupancyTransitions, err = meter.Int64Counter("occupancy_transitions_total",
		metric.WithDescription("Occupancy state transitions by room and target state")); err != nil {
		return err
	}
	instrumentsPtr.Store(ins)
	return nil
}

func registerObservables(meter metric.Meter, p StateProviders) error {
	roomOccupied, err := meter.Int64ObservableGauge("room_occupied",
		metric.WithDescription("1 if the room is occupied, else 0"))
	if err != nil {
		return err
	}
	roomMotion, err := meter.Int64ObservableGauge("room_motion",
		metric.WithDescription("1 if motion is active in the room, else 0"))
	if err != nil {
		return err
	}
	roomSince, err := meter.Int64ObservableGauge("room_seconds_since_occupied",
		metric.WithDescription("Seconds since the room was last marked occupied"), metric.WithUnit("s"))
	if err != nil {
		return err
	}
	channelDepth, err := meter.Int64ObservableGauge("channel_queue_depth",
		metric.WithDescription("Current buffered length of internal pipeline channels"))
	if err != nil {
		return err
	}
	wsClients, err := meter.Int64ObservableGauge("websocket_clients",
		metric.WithDescription("Connected websocket UI clients"))
	if err != nil {
		return err
	}
	mqttConnected, err := meter.Int64ObservableGauge("mqtt_connected",
		metric.WithDescription("1 if the MQTT client is connected, else 0"))
	if err != nil {
		return err
	}

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		if p.RoomStates != nil {
			for _, rs := range p.RoomStates() {
				attrs := metric.WithAttributes(attribute.String("room", rs.Name))
				o.ObserveInt64(roomOccupied, boolToInt(rs.Occupied), attrs)
				o.ObserveInt64(roomMotion, boolToInt(rs.Motion), attrs)
				o.ObserveInt64(roomSince, rs.SecondsSinceOccupied, attrs)
			}
		}
		if p.ChannelDepths != nil {
			for name, depth := range p.ChannelDepths() {
				o.ObserveInt64(channelDepth, int64(depth), metric.WithAttributes(attribute.String("channel", name)))
			}
		}
		if p.WSClients != nil {
			o.ObserveInt64(wsClients, int64(p.WSClients()))
		}
		if p.MQTTConnected != nil {
			o.ObserveInt64(mqttConnected, boolToInt(p.MQTTConnected()))
		}
		return nil
	}, roomOccupied, roomMotion, roomSince, channelDepth, wsClients, mqttConnected)
	return err
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// ---- recording helpers (all nil-safe) --------------------------------------

// RecordDetection records object-detection latency and outcome.
func RecordDetection(model, status string, dur time.Duration) {
	ins := instrumentsPtr.Load()
	if ins == nil {
		return
	}
	ins.detectionDuration.Record(metricsCtx, dur.Seconds(),
		metric.WithAttributes(attribute.String("model", model), attribute.String("outcome", status)))
	ins.detectionRequests.Add(metricsCtx, 1,
		metric.WithAttributes(attribute.String("model", model), attribute.String("status", status)))
}

// RecordPreprocess records image preprocessing latency.
func RecordPreprocess(dur time.Duration) {
	if ins := instrumentsPtr.Load(); ins != nil {
		ins.preprocessDuration.Record(metricsCtx, dur.Seconds())
	}
}

// RecordMessageReceived counts an inbound MQTT message by topic type.
func RecordMessageReceived(msgType string) {
	if ins := instrumentsPtr.Load(); ins != nil {
		ins.messagesReceived.Add(metricsCtx, 1, metric.WithAttributes(attribute.String("type", msgType)))
	}
}

// RecordPublish counts a publish outcome and (for completed attempts) its latency.
func RecordPublish(result string, dur time.Duration) {
	ins := instrumentsPtr.Load()
	if ins == nil {
		return
	}
	ins.messagesPublished.Add(metricsCtx, 1, metric.WithAttributes(attribute.String("result", result)))
	if dur > 0 {
		ins.publishDuration.Record(metricsCtx, dur.Seconds())
	}
}

// RecordImageSkipped counts an image dropped before inference.
func RecordImageSkipped(reason string) {
	if ins := instrumentsPtr.Load(); ins != nil {
		ins.imagesSkipped.Add(metricsCtx, 1, metric.WithAttributes(attribute.String("reason", reason)))
	}
}

// RecordCamFetch records a camera snapshot fetch outcome and latency.
func RecordCamFetch(result string, dur time.Duration) {
	ins := instrumentsPtr.Load()
	if ins == nil {
		return
	}
	ins.camFetch.Add(metricsCtx, 1, metric.WithAttributes(attribute.String("result", result)))
	ins.camFetchDuration.Record(metricsCtx, dur.Seconds())
}

// RecordConfigReload counts an applied configuration reload.
func RecordConfigReload() {
	if ins := instrumentsPtr.Load(); ins != nil {
		ins.configReloads.Add(metricsCtx, 1)
	}
}

// RecordObject counts a detected object and records its confidence, by room and class.
func RecordObject(room, object string, confidence float64) {
	ins := instrumentsPtr.Load()
	if ins == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("room", room), attribute.String("object", object))
	ins.objectsDetected.Add(metricsCtx, 1, attrs)
	ins.objectConfidence.Record(metricsCtx, confidence, attrs)
}

// RecordPersonDetection counts a person-positive frame for a room.
func RecordPersonDetection(room string) {
	if ins := instrumentsPtr.Load(); ins != nil {
		ins.personDetections.Add(metricsCtx, 1, metric.WithAttributes(attribute.String("room", room)))
	}
}

// RecordOccupancyTransition counts a room occupancy state change.
func RecordOccupancyTransition(room, toState string) {
	if ins := instrumentsPtr.Load(); ins != nil {
		ins.occupancyTransitions.Add(metricsCtx, 1,
			metric.WithAttributes(attribute.String("room", room), attribute.String("to_state", toState)))
	}
}
