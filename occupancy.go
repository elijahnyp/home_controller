package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	. "github.com/elijahnyp/home_controller/util"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
)

type ai_result struct {
	Label      string  `json:"label"`
	Confidence float32 `json:"confidence"`
	Y_min      int     `json:"y_min"`
	X_min      int     `json:"x_min"`
	X_max      int     `json:"x_max"`
	Y_max      int     `json:"y_max"`
}

type ai_results struct {
	Predictions []ai_result `json:"predictions"`
	Timestamp   int64       `json:"timestamp"`
	Success     bool        `json:"success"`
}

type ImageCacheItem struct {
	im      []byte
	results ai_results
}

type MQTT_Item struct { //nolint:govet // struct layout optimized for clarity over memory
	Data            []byte
	Topic           string
	Room            string
	Type            int
	Analysis_result int
}

var last_processed = make(map[string]int64)

// Shared state (cache, last_occupancy_state, last_motion_state) and the model
// config pointer now live in state_sync.go behind synchronization.

var cam_forwarder CamForwarder

/* ***************************************
Message Router
*/

/* *************************************** */

/* ***************************************
Routines, dependencies, and Routine Init
*/

// channels
var image_channel = make(chan MQTT_Item, 10)
var results_channel = make(chan MQTT_Item, 10)
var motion_channel = make(chan MQTT_Item, 10)
var door_channel = make(chan MQTT_Item, 10)

// inferenceSem bounds the number of images processed by Triton at once,
// preventing unbounded goroutine/RPC fan-out when images arrive faster than the
// inference server can keep up. Its capacity is set from the
// `inference_concurrency` config at startup (see Init); it is initialized to the
// default here so it is never nil.
const defaultInferenceConcurrency = 4

var inferenceSem = make(chan struct{}, defaultInferenceConcurrency)

// image processing: a single dispatcher applies the per-topic throttle (so the
// last_processed map stays goroutine-confined) then hands work to the bounded
// inference pool.
func ProcessImageRoutine() {
	for {
		item := <-image_channel
		now := time.Now().Unix()
		if last_processed[item.Topic] < now-Config.GetInt64("Frequency") {
			last_processed[item.Topic] = now
			Logger.Debug().Msgf("Processing image from %s", item.Topic)
			inferenceSem <- struct{}{}
			go func(it MQTT_Item) {
				defer func() { <-inferenceSem }()
				ProcessImage(it)
			}(item)
		} else {
			Logger.Debug().Msgf("Skipping image from %s", item.Topic)
			RecordImageSkipped("throttled")
		}
	}
}

func ProcessImage(mimage MQTT_Item) {
	detections, err := DetectObjects(mimage.Data)
	if err != nil {
		Logger.Warn().Msgf("Triton inference error for %s: %v", mimage.Topic, err)
		return
	}

	// Translate TritonDetection → ai_result so the rest of the codebase
	// (cache, API, markup) continues to work without changes.
	var results ai_results
	for _, d := range detections {
		results.Predictions = append(results.Predictions, ai_result{
			Label:      d.Label,
			Confidence: d.Confidence,
			X_min:      d.XMin,
			Y_min:      d.YMin,
			X_max:      d.XMax,
			Y_max:      d.YMax,
		})
	}
	results.Timestamp = time.Now().Unix()
	results.Success = true

	CacheSet(mimage.Topic, ImageCacheItem{mimage.Data, results})

	var person = false
	var confidence float32
	minConf := float32(Config.GetFloat64("min_confidence"))
	if minConf <= 0 {
		minConf = 0.5
	}
	for _, r := range results.Predictions {
		RecordObject(mimage.Room, r.Label, float64(r.Confidence))
		if r.Label == "person" {
			person = true
			if confidence < r.Confidence {
				confidence = r.Confidence
			}
		}
	}

	if person && confidence >= minConf {
		Logger.Debug().Msgf("%s occupied: %.3f", mimage.Topic, confidence)
		RecordPersonDetection(mimage.Room)
		mimage.Analysis_result = OCCUPIED
	} else {
		Logger.Debug().Msgf("%s unoccupied", mimage.Topic)
		mimage.Analysis_result = UNOCCUPIED
	}
	results_channel <- mimage
}

// occupancy management

func OccupancyManagerRoutine() {
	/*
		remember, motion, cam, and door messages are separate, so only one type is
		checked at a time.
		occupancy on comes from motion, cam (person), or a door opening
		occupancy off comes from expired cam period AND motion off

		Basically, cameras are checked every x seconds and must not see a person in y seconds to say 'no person'
		Motion (and a door opening) resets the period as does seeing a person.
		Unoccupied only triggered if cam says 'no person' and motion is off
	*/
	for {
		item := <-results_channel // pickup work
		now := time.Now().Unix()
		occupancy_topic := CurrentModel().FindOccupancyTopicByRoom(item.Room)
		cam_opinion := true
		var message string
		room := CurrentModel().GetRoomStatus(item.Room)
		switch item.Analysis_result {
		case OCCUPIED:
			room.Occupied()
			cam_opinion = true
		case UNOCCUPIED:
			if room.GetLastOccupied() < now-CurrentModel().RoomOccupancyPeriod(item.Room) {
				Logger.Debug().Msgf("%s OCCUPANCY PERIOD EXPIRED", item.Room)
				room.Unoccupied()
				cam_opinion = false
			} else {
				cam_opinion = true
			}
		case MOTION_START:
			room.Occupied()
			room.Motion(true)
		case MOTION_STOP:
			room.Motion(false)
		case DOOR_OPEN:
			// Intent: an opening door marks the room occupied and resets the
			// timer, without asserting continuous motion.
			room.Occupied()
		}
		if !cam_opinion && !room.GetMotionState() {
			message = "false"
		} else {
			message = "true"
		}

		occupied := message == "true"

		// Record occupancy transitions before updating the stored state.
		if prev, existed := GetOccupancyState(item.Room); !existed || prev != occupied {
			if occupied {
				RecordOccupancyTransition(item.Room, "occupied")
			} else {
				RecordOccupancyTransition(item.Room, "unoccupied")
			}
		}

		// Update web-facing state.
		SetOccupancyState(item.Room, occupied)

		// Broadcast update via WebSocket if available
		if wsHub != nil {
			wsHub.BroadcastUpdate("room_status", map[string]interface{}{
				"room":            item.Room,
				"occupied":        occupied,
				"motion":          room.GetMotionState(),
				"person_detected": cam_opinion,
			})
		}

		CurrentModel().UpdateRoomStatus(item.Room, room)
		PublishAsync(occupancy_topic, byte(0), false, []byte(message))
	}
}

// setMotionTracking records the motion state for a matching motion topic.
func setMotionTracking(room, topic string, on bool) {
	for _, r := range CurrentModel().Rooms {
		if r.Name == room {
			for _, t := range r.Motion_topics {
				if t == topic {
					SetMotionState(topic, on)
				}
			}
		}
	}
}

func MotionManagerRoutine() {
	for {
		item := <-motion_channel
		if numd, err := strconv.Atoi(string(item.Data)); err == nil {
			Logger.Debug().Msgf("%s motion integer received: %d", item.Room, numd)
			if numd == 0 {
				item.Analysis_result = MOTION_STOP
				setMotionTracking(item.Room, item.Topic, false)
			} else {
				item.Analysis_result = MOTION_START
				setMotionTracking(item.Room, item.Topic, true)
			}
			results_channel <- item
			continue
		}
		Logger.Debug().Msgf("%s motion string received: %s", item.Room, string(item.Data))
		switch string(item.Data) {
		case "OFF":
			item.Analysis_result = MOTION_STOP
			setMotionTracking(item.Room, item.Topic, false)
			results_channel <- item
		case "ON":
			item.Analysis_result = MOTION_START
			setMotionTracking(item.Room, item.Topic, true)
			results_channel <- item
		case "OPEN":
			// A door reported via a motion topic: treat an opening as an
			// occupancy trigger (see DoorManagerRoutine for dedicated door topics).
			item.Analysis_result = DOOR_OPEN
			results_channel <- item
		case "CLOSED":
			// A closing door is not itself an occupancy signal; the cam/motion
			// timers drive the room back to unoccupied.
			Logger.Debug().Msgf("%s door closed (no-op)", item.Room)
		default:
			Logger.Debug().Msgf("%s unrecognized motion payload: %s", item.Room, string(item.Data))
		}
	}
}

// DoorManagerRoutine consumes dedicated door topics. An opening door marks the
// room occupied; a closing door is a no-op (timers handle un-occupancy).
func DoorManagerRoutine() {
	for {
		item := <-door_channel
		payload := string(item.Data)
		Logger.Debug().Msgf("%s door event received: %s", item.Room, payload)
		switch payload {
		case "OPEN", "ON", "1", "true":
			item.Analysis_result = DOOR_OPEN
			results_channel <- item
		case "CLOSED", "OFF", "0", "false":
			Logger.Debug().Msgf("%s door closed (no-op)", item.Room)
		default:
			Logger.Debug().Msgf("%s unrecognized door payload: %s", item.Room, payload)
		}
	}
}

type point struct {
	x int
	y int
}

type MarkupSpec struct {
	label      string
	min        point
	max        point
	confidence float32
}

func MarkupImage(imgsource image.Image, specs []MarkupSpec) image.Image {
	line_width := 5
	line_length := 60
	imgboxes := image.NewRGBA(imgsource.Bounds())
	for x := 0; x < imgsource.Bounds().Max.X; x++ {
		for y := 0; y < imgsource.Bounds().Max.Y; y++ {
			imgboxes.Set(x, y, imgsource.At(x, y))
		}
	}
	for _, spec := range specs {
		start := spec.min
		end := spec.max
		label := spec.label
		// top left
		for x := start.x; x < start.x+60; x++ {
			for y := start.y; y < start.y+line_width; y++ {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		for x := start.x; x < start.x+line_width; x++ {
			for y := start.y; y < start.y+line_length; y++ {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		// bottom right
		for x := end.x; x > end.x-line_length; x-- {
			for y := end.y; y > end.y-line_width; y-- {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		for x := end.x; x > end.x-line_width; x-- {
			for y := end.y; y > end.y-line_length; y-- {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		// top right
		for x := end.x; x > end.x-line_length; x-- {
			for y := start.y; y < start.y+line_width; y++ {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		for x := end.x; x > end.x-line_width; x-- {
			for y := start.y; y < start.y+line_length; y++ {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		// bottom left
		for x := start.x; x < start.x+line_length; x++ {
			for y := end.y; y > end.y-line_width; y-- {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		for x := start.x; x < start.x+line_width; x++ {
			for y := end.y; y > end.y-line_length; y-- {
				imgboxes.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}

		color := color.RGBA{255, 0, 0, 255}
		d := &font.Drawer{
			Dst:  imgboxes,
			Src:  image.NewUniform(color),
			Face: inconsolata.Bold8x16,
			Dot:  fixed.Point26_6{X: fixed.I(start.x), Y: fixed.I(start.y - 3)},
		}

		d.DrawString(fmt.Sprintf("%s - %.03f", label, spec.confidence))
	}

	return imgboxes
}

func HttpImage(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Error parsing form", http.StatusBadRequest)
			return
		}
		id := r.FormValue("id")
		cacheitem, _ := CacheGet(id)
		if cacheitem.im != nil {
			var spec []MarkupSpec
			for _, i := range cacheitem.results.Predictions {
				s := MarkupSpec{i.Label, point{i.X_min, i.Y_min}, point{i.X_max, i.Y_max}, i.Confidence}
				spec = append(spec, s)
			}
			imgsource, err := jpeg.Decode(bytes.NewReader(cacheitem.im))
			if err != nil {
				http.Error(w, "Error decoding image", http.StatusInternalServerError)
				return
			}
			imgboxes := MarkupImage(imgsource, spec)
			w.Header().Add("Content-Type", "image/jpeg")
			imgWriter := bytes.NewBuffer(nil)
			if err := jpeg.Encode(imgWriter, imgboxes, nil); err != nil {
				http.Error(w, "Error encoding image", http.StatusInternalServerError)
				return
			}
			if _, err := w.Write(imgWriter.Bytes()); err != nil {
				Logger.Error().Msgf("Error writing image response: %v", err)
			}
		} else {
			w.WriteHeader(404)
			if _, err := io.WriteString(w, "Unknown ID"); err != nil {
				Logger.Error().Msgf("Error writing response: %v", err)
			}
		}
	} else {
		w.WriteHeader(400)
		if _, err := io.WriteString(w, "Bad Request Method\n"); err != nil {
			Logger.Error().Msgf("Error writing response: %v", err)
		}
	}
}

func RoomOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Error parsing form", http.StatusBadRequest)
			return
		}
		room := r.FormValue("room")
		for _, r := range CurrentModel().Rooms {
			if r.Name == room {
				w.Header().Add("Content-Type", "text/html")
				writeString := func(s string) {
					if _, err := io.WriteString(w, s); err != nil {
						Logger.Error().Msgf("Error writing response: %v", err)
					}
				}
				writeString("<html><body>")
				for _, t := range r.Pic_topics {
					writeString(fmt.Sprintf("<h3>%s</h3>", t))
					writeString(fmt.Sprintf("<img src=\"/image?id=%s\" /><br>", t))
				}
				writeString("</body></html>")
			}
		}
	}
}

func StatusOverview(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	if r.Method == "GET" {
		w.Header().Add("Content-Type", "text/html")
		writeString := func(s string) {
			if _, err := io.WriteString(w, s); err != nil {
				Logger.Error().Msgf("Error writing response: %v", err)
			}
		}
		writeString("<html><body><table>")
		writeString("<tr><th>Room</th><th>Last Occupied (seconds ago)</th><th>Motion State</th><th>Timeout</th></tr>")
		for room, status := range CurrentModel().SnapshotRoomStatuses() {
			writeString("<tr>")
			writeString(fmt.Sprintf("<td><a href=\"/room?room=%s\">%s</a></td>", room, room))
			writeString(fmt.Sprintf("<td>%d</td>", now-status.GetLastOccupied()))
			writeString(fmt.Sprintf("<td>%v</td>", status.GetMotionState()))
			writeString(fmt.Sprintf("<td>%d</td>", CurrentModel().RoomOccupancyPeriod(room)))
			writeString("</tr>")
		}
		writeString("</body></html>")
	}
}

type modelapiresponseitem struct {
	AI   map[string]ai_results `json:"ai"`
	Room Room                  `json:"room"`
}

func ModelApi(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		answer := make(map[string]modelapiresponseitem)
		if err := r.ParseForm(); err != nil {
			http.Error(w, fmt.Sprintf("Error parsing form: %v", err), http.StatusBadRequest)
			return
		}
		room := r.FormValue("room")
		if room != "" {
			for _, r := range CurrentModel().Rooms {
				if r.Name == room {
					ai := make(map[string]ai_results)
					for _, t := range r.Pic_topics {
						ci, _ := CacheGet(t)
						ai[t] = ci.results
					}
					w.Header().Add("Content-Type", "application/json")
					answer[r.Name] = modelapiresponseitem{
						Room: r,
						AI:   ai,
					}
					data, err := json.Marshal(answer)
					if err != nil {
						http.Error(w, fmt.Sprintf("Error marshaling response: %v", err), http.StatusInternalServerError)
						return
					}
					if _, err := w.Write(data); err != nil {
						Logger.Error().Msgf("Error writing response: %v", err)
					}
					return
				}
			}
		} else {
			for _, r := range CurrentModel().Rooms {
				ai := make(map[string]ai_results)
				for _, t := range r.Pic_topics {
					ci, _ := CacheGet(t)
					ai[t] = ci.results
				}
				w.Header().Add("Content-Type", "application/json")
				answer[r.Name] = modelapiresponseitem{
					Room: r,
					AI:   ai,
				}
			}
			data, err := json.Marshal(answer)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error marshaling response: %v", err), http.StatusInternalServerError)
				return
			}
			if _, err := w.Write(data); err != nil {
				Logger.Error().Msgf("Error writing response: %v", err)
			}
			return
		}
		w.WriteHeader(404)
		if _, err := io.WriteString(w, "Room not found"); err != nil {
			Logger.Error().Msgf("Error writing error response: %v", err)
		}
	} else {
		w.WriteHeader(400)
		if _, err := io.WriteString(w, "Bad Request Method\n"); err != nil {
			Logger.Error().Msgf("Error writing error response: %v", err)
		}
	}
}

// init
func Init() {
	if err := InitTritonClient(); err != nil {
		Logger.Fatal().Msgf("Failed to initialize Triton client: %v", err)
	}
	concurrency := Config.GetInt("inference_concurrency")
	if concurrency <= 0 {
		concurrency = defaultInferenceConcurrency
	}
	inferenceSem = make(chan struct{}, concurrency)
	Logger.Info().Msgf("inference concurrency set to %d", concurrency)
	StartPublisher()
	go ProcessImageRoutine()
	go OccupancyManagerRoutine()
	go MotionManagerRoutine()
	go DoorManagerRoutine()
}

// subscribedTopics tracks the topics we currently hold an MQTT subscription for,
// so a config reload can unsubscribe topics that were removed rather than leaking
// stale subscriptions.
var (
	subscribedMu     sync.Mutex
	subscribedTopics = make(map[string]bool)
)

func subscribeOccupancyTopics() {
	subscribedMu.Lock()
	defer subscribedMu.Unlock()

	desired := make(map[string]bool)
	for _, topic := range CurrentModel().SubscribeTopics() {
		desired[topic] = true
	}

	connected := Client != nil && Client.IsConnected()

	// Subscribe to newly desired topics.
	for topic := range desired {
		if subscribedTopics[topic] {
			continue
		}
		Logger.Debug().Msgf("Registering subscription for topic: %s", topic)
		RegisterMQTTSubscription(topic, receiver)
		if connected {
			if token := Client.Subscribe(topic, 0, receiver); token.Wait() && token.Error() != nil {
				Logger.Error().Msgf("Error subscribing to %s: %v", topic, token.Error())
				continue
			}
			Logger.Info().Msgf("Subscribed to topic: %s", topic)
		}
		subscribedTopics[topic] = true
	}

	// Unsubscribe from topics that are no longer in the config.
	for topic := range subscribedTopics {
		if desired[topic] {
			continue
		}
		Logger.Info().Msgf("Unsubscribing from removed topic: %s", topic)
		RegisterMQTTSubscription(topic, nil)
		if connected {
			if token := Client.Unsubscribe(topic); token.Wait() && token.Error() != nil {
				Logger.Error().Msgf("Error unsubscribing from %s: %v", topic, token.Error())
			}
		}
		delete(subscribedTopics, topic)
	}
}

func receiver(client MQTT.Client, message MQTT.Message) {
	Logger.Info().Msgf("Message Received on topic %s", message.Topic())
	var mitem MQTT_Item
	mitem.Data = message.Payload()
	mitem.Topic = message.Topic()
	mitem.Room = CurrentModel().FindRoomByTopic(message.Topic())
	switch CurrentModel().FindTopicType(message.Topic()) {
	case PIC:
		mitem.Type = PIC
		RecordMessageReceived("pic")
		Logger.Debug().Msgf("image message received: queue len %v", len(image_channel))
		image_channel <- mitem
	case MOTION:
		mitem.Type = MOTION
		RecordMessageReceived("motion")
		Logger.Debug().Msgf("motion message received: queue len %v", len(motion_channel))
		motion_channel <- mitem
	case OCCUPANCY:
		mitem.Type = OCCUPANCY
		RecordMessageReceived("occupancy")
	case DOOR:
		mitem.Type = DOOR
		RecordMessageReceived("door")
		Logger.Debug().Msgf("door message received: queue len %v", len(door_channel))
		door_channel <- mitem
	default:
		RecordMessageReceived("unknown")
		Logger.Debug().Msgf("topic %s not found in model.  Fix subscription or add to model", message.Topic())
	}
}

func main() {
	LogInit("trace")
	SetupConfig()
	RegisterNewConfigListener(func() { LogInit(Config.GetString("log_level")) })
	RegisterNewConfigListener(func() {
		var m Model
		if err := m.BuildModel(); err != nil {
			Logger.Error().Msgf("Error building model: %v", err)
			return
		}
		SetModel(&m)
		RecordConfigReload()
	})
	RegisterNewConfigListener(subscribeOccupancyTopics)
	RegisterNewConfigListener(func() {
		if err := InitTritonClient(); err != nil {
			Logger.Error().Msgf("Failed to reinitialize Triton client on config change: %v", err)
		}
	})
	RegisterMQTTConnectHook("haadvertise", func(_ MQTT.Client) {
		AdvertiseHA(CurrentModel().Rooms)
	})
	RegisterNewConfigListener(MqttInit)
	if Config.GetBool("insecure_tls") {
		Logger.Debug().Msg("disabling tls")
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // intentional for testing environments
		} else {
			Logger.Warn().Msg("Failed to configure insecure TLS: transport type assertion failed")
		}
	}
	OnNewConfig()
	Init()
	monitor := NewMonitorServer()

	// Legacy endpoints (keep intact as per requirements)
	monitor.AddHandler("/image", HttpImage)
	monitor.AddHandler("/room", RoomOverview)
	monitor.AddHandler("/room_status", StatusOverview)
	monitor.AddHandler("/model", ModelApi)

	// New web interface endpoints
	monitor.AddHandler("/", HomeHandler)
	monitor.AddHandler("/ws", ServeWebSocket)
	monitor.AddHandler("/api/status", APISystemStatus)
	monitor.AddHandler("/api/room", APIRoomDetail)
	monitor.AddHandler("/room_detail", RoomDetailHandler)

	// Prometheus metrics endpoint
	if metricsHandler, err := InitMetrics(MetricProviders()); err != nil {
		Logger.Error().Msgf("Error initializing metrics: %v", err)
	} else {
		monitor.AddRawHandler("/metrics", metricsHandler)
	}
	if err := monitor.Start(); err != nil {
		Logger.Error().Msgf("Error starting monitor server: %v", err)
	}
	RegisterNewConfigListener(func() { monitor.Restart() })
	cam_forwarder.MakeCamForwarder()
	cam_forwarder.Start()
	Logger.Info().Msg("ready")
	go OnlinePinger() // start the online pinger
	go HAAdvertiser() // start the HA advertisement pinger
	select {}         // block forever
}

// online pinger
func OnlinePinger() {
	for {
		PublishAsync("hab/online", 0, false, []byte("online"))
		time.Sleep(10 * time.Second)
	}
}

// HAAdvertiser - advertises Home Assistant discovery messages every 5 minutes
func HAAdvertiser() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if Client != nil && Client.IsConnected() {
			Logger.Debug().Msg("Advertising Home Assistant discovery messages")
			AdvertiseHA(CurrentModel().Rooms)
		}
	}
}
