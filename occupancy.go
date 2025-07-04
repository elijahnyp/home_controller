package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"mime/multipart"
	"bytes"
	"time"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
	"image/color"
	"image"
	"image/jpeg"
	"crypto/tls"
	"strconv"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	. "github.com/elijahnyp/home_controller/util"
)

type ai_result struct {
	Confidence float32 `json:"confidence"`
	Label string `json:"label"`
	Y_min int `json:"y_min"`
	X_min int `json:"x_min"`
	X_max int `json:"x_max"`
	Y_max int `json:"y_max"`
}

type ai_results struct {
	Success bool `json:"success"`
	Timestamp int64 `json:"timestamp"`
	Predictions []ai_result `json:"predictions"`
}

type ImageCacheItem struct {
	im []byte
	results ai_results
}

type MQTT_Item struct {
	Room string
	Data []byte
	Topic string
	Type int
	Analysis_result int
}

var last_processed = make(map[string]int64)
var cache = make(map[string]ImageCacheItem)

var model Model

var cam_forwarder CamForwarder

/* ***************************************
Message Router
*/

/* *************************************** */



/* ***************************************
Routines, dependencies, and Routine Init
*/

//channels
var image_channel = make(chan MQTT_Item, 10)
var results_channel = make(chan MQTT_Item, 10)
var motion_channel = make(chan MQTT_Item, 10)
var door_channel = make(chan MQTT_Item, 10)

// //door processing
// func ProcessDoorRoutine(){
// 	for {
// 		item := <- door_channel
		
// 	}
// }

//image processing
func ProcessImageRoutine(){
	for {
		item := <- image_channel
		now := time.Now().Unix()
		if last_processed[item.Topic] < now - Config.GetInt64("Frequency") {
			last_processed[item.Topic] = now
			Logger.Debug().Msgf("Processing image from %s", item.Topic)
			go ProcessImage(item)
		} else {
			Logger.Debug().Msgf("Skipping image from %s", item.Topic)
		}
	}
}

func ProcessImage(mimage MQTT_Item) {
	//create form for upload
	upload_body := bytes.NewBuffer(nil)
	multipartWriter := multipart.NewWriter(upload_body)
	part, err := multipartWriter.CreateFormFile("image", "snap.jpeg")
	if err != nil {
		Logger.Error().Msgf("Error reading image: %v",err.Error())
	}

	// copy image into form
	copied, err := io.Copy(part, bytes.NewReader(mimage.Data))
	if err != nil {
		Logger.Error().Msgf("Error copying image into form: %v",err.Error())
	} else {
		if copied < 1 {
			Logger.Warn().Msg("empty copying image into form but no error reported")
		}
	}

	// set minimum confidence
	// multipartWriter.WriteField("min_confidence", "0.5")
	multipartWriter.Close() //must close or http client doesn't put in content length - can't use defer
	// send request
	req, err := http.NewRequest("POST", Config.GetString("detection_url"), upload_body)
	if err != nil {
		Logger.Warn().Msgf("Error posting form to ai server %v", err.Error())
		return
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		Logger.Warn().Msgf("Error reading result from ai server: %v", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode > 299 || resp.StatusCode < 200{
		Logger.Warn().Msgf("non-2xx code received: %d",resp.StatusCode)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	var results ai_results
	err = json.Unmarshal(body, &results)
	if err != nil {
		Logger.Warn().Msgf("Unable to unmarshal ai result: %v", err.Error())
		return
	}
	results.Timestamp = time.Now().Unix()
	//cache the bloody thing
	cache[mimage.Topic] = ImageCacheItem{mimage.Data,results}
	var person = false
	var confidence float32 = 0.0
	for _, result := range results.Predictions{
		if result.Label == "person"{
			person = true
			if confidence < result.Confidence {
				confidence = result.Confidence
			}
			break
		}
	}
	if person && confidence >= float32(Config.GetFloat64("min_confidence")){
		Logger.Debug().Msgf("%s occupied: %f", mimage.Topic, confidence)
		// last_occupied[mimage.Topic] = now
		mimage.Analysis_result = OCCUPIED
	} else {
		Logger.Debug().Msgf("%s unoccupied", mimage.Topic)
		mimage.Analysis_result = UNOCCUPIED
	}
	results_channel <- mimage
}

//occupancy management

func OccupancyManagerRoutine() {
	/* 
	remember, motion and cam messages are separate, so only one type is checked at a time
	occupancy on comes from either motion or cam
	occupancy off comes from expired cam period AND motion off

	Basically, cameras are checked every x seconds and must not see a person in y seconds to say 'no person'
	Motion resets the period as does seeing a person.
	Unoccupied only triggered if cam says 'no person' and motion is off
	*/
	for {
		item := <- results_channel // pickup work
		now := time.Now().Unix()
		occupancy_topic := model.FindOccupancyTopicByRoom(item.Room)
		cam_opinion := true
		var message string
		room := model.ModelStatus().Room_status[item.Room]
		if item.Analysis_result == OCCUPIED {
			// if room.last_occupied <= now { //do this so we can set future occupied
			// 	room.last_occupied = now
			// }
			room.Occupied()
			cam_opinion = true
		} else if item.Analysis_result == UNOCCUPIED {
			if room.GetLastOccupied() < now - model.RoomOccupancyPeriod(item.Room) {
				Logger.Debug().Msgf("%s OCCUPANCY PERIOD EXPIRED", item.Room)
				room.Unoccupied()
				cam_opinion = false
			} else {
				cam_opinion = true
			}
		}
		if item.Analysis_result == MOTION_START {
			// if room.last_occupied <= now { //do this so we can set future occupied
			// 	room.last_occupied = now
			// }
			room.Occupied()
			// room.motion_state = true
			room.Motion(true)
		} else if item.Analysis_result == MOTION_STOP{
			//  room.motion_state = false
			room.Motion(false)
		} 
		if ! cam_opinion && ! room.GetMotionState() {
			message = "false"
		} else {
			message = "true"
		}
		model.UpdateRoomStatus(item.Room, room)
		token := Client.Publish(occupancy_topic, byte(0), false, message)
		token.Wait() //this is VERY BAD
	}
}

func MotionManagerRoutine() {
	for {
		item := <- motion_channel
		//process data - handle multiple on options?
		// Logger.Debug().Msgf("%s motion %s", item.Room, string(item.Data))
		if numd, err := strconv.Atoi(string(item.Data)); err == nil {
			Logger.Debug().Msgf("%s motion integer received: %d",item.Room,numd)
			switch numd{
			case 0:
				item.Analysis_result = MOTION_STOP
				results_channel <- item
				continue
			default:
				item.Analysis_result = MOTION_START
				results_channel <- item
				continue
			}
		} else {
			Logger.Debug().Msgf("%s motion string received: %s",item.Room, string(item.Data))
			if string(item.Data) == "OFF" || string(item.Data) == "OPEN"{
				item.Analysis_result = MOTION_STOP
				results_channel <- item
				continue
			} else if string(item.Data) == "ON" || string(item.Data) == "CLOSED"{
				item.Analysis_result = MOTION_START
				results_channel <- item
				continue
			}
		}
	}
}

type point struct {
	x int
	y int
}

type MarkupSpec struct {
	min point
	max point
	label string
	confidence float32
}

func MarkupImage(imgsource image.Image, specs []MarkupSpec) (image.Image) {
	line_width := 5
	line_length := 60
	imgboxes := image.NewRGBA(imgsource.Bounds())
	for x := 0; x < imgsource.Bounds().Max.X; x++ {
		for y := 0; y < imgsource.Bounds().Max.Y; y++{
			imgboxes.Set(x,y,imgsource.At(x,y))
		}
	}
	for _, spec := range(specs) {
		start := spec.min
		end := spec.max
		label := spec.label
		// top left
		for x := start.x; x < start.x + 60; x++ {
			for y := start.y; y < start.y + line_width; y++{
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			} 
		}
		for x := start.x; x < start.x + line_width; x++{
			for y := start.y; y < start.y + line_length; y++ {
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			}
		}
		// bottom right
		for x := end.x; x > end.x - line_length; x--{
			for y := end.y; y > end.y - line_width; y-- {
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			}
		}
		for x := end.x; x > end.x - line_width; x--{
			for y := end.y; y > end.y - line_length; y-- {
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			}
		}
		// top right
		for x := end.x; x > end.x - line_length; x--{
			for y := start.y; y < start.y + line_width; y++ {
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			}
		}
		for x := end.x; x > end.x - line_width; x--{
			for y := start.y; y < start.y + line_length; y++ {
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			}
		}
		// bottom left
		for x := start.x; x < start.x + line_length; x++{
			for y := end.y; y > end.y - line_width; y-- {
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			}
		}
		for x := start.x; x < start.x + line_width; x++{
			for y := end.y; y > end.y - line_length; y-- {
				imgboxes.Set(x, y, color.RGBA{255,0,0,255})
			}
		}

		color := color.RGBA{255,0,0,255}
		d := &font.Drawer{
			Dst: imgboxes,
			Src: image.NewUniform(color),
			Face: inconsolata.Bold8x16,
			Dot: fixed.Point26_6{fixed.I(start.x), fixed.I(start.y - 3)},
		}

		d.DrawString(fmt.Sprintf("%s - %.03f",label, spec.confidence))
	}

	return imgboxes
}

func HttpImage(w http.ResponseWriter, r *http.Request){
	if r.Method == "GET" {
		r.ParseForm()
		id := r.FormValue("id")
		cacheitem := cache[id]
		if cacheitem.im != nil {
			var spec []MarkupSpec
			for _, i := range cacheitem.results.Predictions {
				s := MarkupSpec{point{i.X_min,i.Y_min}, point{i.X_max, i.Y_max}, i.Label, i.Confidence}
				spec = append(spec,s)
			}
			imgsource, _ := jpeg.Decode(bytes.NewReader(cacheitem.im))
			imgboxes := MarkupImage(imgsource,spec)
			w.Header().Add("Content-Type", "image/jpeg")
			imgWriter := bytes.NewBuffer(nil)
			jpeg.Encode(imgWriter, imgboxes, nil)
			w.Write(imgWriter.Bytes())
		} else {
			w.WriteHeader(404)
			io.WriteString(w, "Unknown ID")
		}
	} else {
		w.WriteHeader(400)
		io.WriteString(w, "Bad Request Method\n")
	}
}

func RoomOverview(w http.ResponseWriter, r *http.Request){
	if r.Method == "GET" {
		r.ParseForm()
		room := r.FormValue("room")
		for _, r := range model.Rooms{
			if r.Name == room {
				w.Header().Add("Content-Type", "text/html")
				io.WriteString(w, "<html><body>")
				for _, t := range r.Pic_topics {
					io.WriteString(w, fmt.Sprintf("<h3>%s</h3>",t))
					io.WriteString(w, fmt.Sprintf("<img src=\"/image?id=%s\" /><br>",t))
				}
				io.WriteString(w, "</body></html>")
			}
		}
	}
}

func StatusOverview(w http.ResponseWriter, r *http.Request){
	now := time.Now().Unix()
	if r.Method == "GET" {
		w.Header().Add("Content-Type", "text/html")
		io.WriteString(w, "<html><body><table>")
		io.WriteString(w,"<tr><th>Room</th><th>Last Occupied (seconds ago)</th><th>Motion State</th><th>Timeout</th></tr>")
		for room, status := range model.ModelStatus().Room_status{
			io.WriteString(w, "<tr>")
			io.WriteString(w, fmt.Sprintf("<td><a href=\"/room?room=%s\">%s</a></td>",room,room))
			io.WriteString(w, fmt.Sprintf("<td>%d</td>",now - status.GetLastOccupied()))
			io.WriteString(w, fmt.Sprintf("<td>%v</td>",status.GetMotionState()))
			io.WriteString(w, fmt.Sprintf("<td>%d</td>",model.RoomOccupancyPeriod(room)))
			io.WriteString(w, "</tr>")
		}
		io.WriteString(w, "</body></html>")
	}
}

type modelapiresponseitem struct {
	Room Room `json:"room"`
	AI map[string]ai_results `json:"ai"`
}

func ModelApi(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		answer := make(map[string]modelapiresponseitem)
		r.ParseForm()
		room := r.FormValue("room")
		if room != "" {
			for _, r := range model.Rooms {
				if r.Name == room {
					ai := make(map[string]ai_results)
					for _, t := range r.Pic_topics {
						ai[t] = cache[t].results
					}
					w.Header().Add("Content-Type", "application/json")
					answer[r.Name] = modelapiresponseitem{
						Room: r,
						AI: ai,
					}
					data, _ := json.Marshal(answer)
					w.Write(data)
					return
				}
			}
		} else {
			for _, r := range model.Rooms {
				ai := make(map[string]ai_results)
				for _, t := range r.Pic_topics {
					ai[t] = cache[t].results
				}
				w.Header().Add("Content-Type", "application/json")
				answer[r.Name] = modelapiresponseitem{
					Room: r,
					AI: ai,
				}
			}
			data, _ := json.Marshal(answer)
			w.Write(data)
			return
		}
		w.WriteHeader(404)
		io.WriteString(w, "Room not found")
	} else {
		w.WriteHeader(400)
		io.WriteString(w, "Bad Request Method\n")
	}
}

//init
func Init(){
	http.DefaultClient.Timeout = 10 * time.Second;
	go ProcessImageRoutine()
	go OccupancyManagerRoutine()
	go MotionManagerRoutine()
}

func subscribeOccupancyTopics(){
	for _, topic := range model.SubscribeTopics() {
		RegisterMQTTSubscription(topic,receiver)
	}
}


func receiver(client MQTT.Client, message MQTT.Message) {
	Logger.Info().Msgf("Message Received on topic %s",message.Topic())
	var mitem MQTT_Item
	mitem.Data = message.Payload()
	mitem.Topic = message.Topic()
	mitem.Room = model.FindRoomByTopic(message.Topic())
	switch model.FindTopicType(message.Topic()) {
	case PIC:
		mitem.Type = PIC
		Logger.Debug().Msgf("image message received: queue len %v",len(image_channel))
		image_channel <- mitem
	case MOTION:
		mitem.Type = MOTION
		Logger.Debug().Msgf("motion message received: queue len %v",len(motion_channel))
		motion_channel <- mitem
		//do something here
	case OCCUPANCY:
		mitem.Type = OCCUPANCY
		//do something here
	case DOOR:
		mitem.Type = DOOR
		Logger.Debug().Msgf("door message received: queue len %v",len(door_channel))
		door_channel <- mitem
	default:
		Logger.Debug().Msgf("topic %s not found in model.  Fix subscription or add to model", message.Topic())
	}
}

func main() {
	LogInit("trace")
	SetupConfig()
	RegisterNewConfigListener(func(){LogInit(Config.GetString("log_level"))})
	RegisterNewConfigListener(func(){model.BuildModel()})
	RegisterNewConfigListener(subscribeOccupancyTopics)
	RegisterMQTTConnectHook("haadvertise", func(client MQTT.Client) {
		AdvertiseHA(model.Rooms, client)
	})
	RegisterNewConfigListener(MqttInit)
	if Config.GetBool("insecure_tls") {
		Logger.Debug().Msg("disabling tls")
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	OnNewConfig()
	Init()
	monitor := NewMonitorServer()
	monitor.AddHandler("/image", HttpImage)
	monitor.AddHandler("/room", RoomOverview)
	monitor.AddHandler("/room_status", StatusOverview)
	monitor.AddHandler("/model", ModelApi)
	monitor.Start()
	RegisterNewConfigListener(func(){monitor.Restart()})
	cam_forwarder.MakeCamForwarder()
	cam_forwarder.Start()
	Logger.Info().Msg("ready")
	go OnlinePinger() //start the online pinger
	select {} //block forever
}

// online pinger
func OnlinePinger() {
	for {
		if token := Client.Publish("hab/online", 0, false, "online"); token.Wait() && token.Error() != nil {
			Logger.Error().Msgf("Error publishing online message: %v", token.Error())
		}
		time.Sleep(10 * time.Second)
	}
}
