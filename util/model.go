package util

import (
	"fmt"
	"time"
)

var model_status *ModelStatus

const ( //message types
	PIC = iota
	MOTION = iota
	OCCUPANCY = iota
	DOOR = iota
)

const ( //analysis results
	OCCUPIED = iota
	UNOCCUPIED = iota
	MOTION_START = iota
	MOTION_STOP = iota
	DOOR_OPEN = iota
	DOOR_CLOSED = iota
)

type Model struct {
	Rooms []Room `mapstructure:"rooms"`
	Location Location `mapstructure:"location"`
	People []Person `mapstructure:"people"`
}

type Location struct {
	Lat float64 `mapstructure:"latitude"`
	Lon float64 `mapstructure:"longitude"`
	Name string `mapstructure:"name"`
}

func (l Location)GetCoordinages() (latitude float64, longitude float64){
	return l.Lat, l.Lon
}

type Person struct {
	Location_topic string `mapstructure:"location_topic"`
	Name string `mapstructure:"name"`
}

type Room struct {
	Name string `mapstructure:"name"`
	Occupancy_topic string `mapstructure:"occupancy_topic"`
	Motion_topics []string `mapstructure:"motion_topics"`
	Pic_topics []string `mapstructure:"pic_topics"`
	Door_topics []string `mapstructure:"door_dopics"`
	Occupancy_period int64 `mapstructure:"occupancy_period"`
}

type RoomStatus struct {
	last_occupied int64
	motion_state bool
	occupied bool
}

type ModelStatus struct {
	Room_status map[string]RoomStatus
}

func (m *RoomStatus) Occupied(){
	now := time.Now().Unix()
	if m.last_occupied <= now {
		m.last_occupied = now
	}
	m.occupied = true
}

func (m *RoomStatus) Unoccupied(){
	m.occupied = false
}

func (m *RoomStatus) Motion(state bool){
	m.motion_state = state
	m.Occupied()
}

func(m *Model) UpdateRoomStatus(room string, item RoomStatus){
	model_status.Room_status[room] = item
}

func (m *RoomStatus) GetLastOccupied() int64{
	return m.last_occupied
}

func (m *RoomStatus) GetMotionState() bool{
	return m.motion_state
}

func newModelStatus() *ModelStatus{
	s := ModelStatus{}
	s.Room_status = make(map[string]RoomStatus)
	return &s
}

func (m Model) FindRoomByTopic(topic string) (string){
	for _, entry := range(m.Rooms){
		if entry.Occupancy_topic == topic{
			 return entry.Name
		}
		for _, mt := range(entry.Motion_topics){
			if mt == topic{
				return entry.Name
			}
		}
		for _, pt := range(entry.Pic_topics){
			if pt == topic{
				return entry.Name
			}
		}
		for _, dt := range(entry.Door_topics){
			if dt == topic{
				return entry.Name
			}
		}
	}
	return ""
}

func (m Model) FindTopicType(topic string) (int){
	for _, entry := range(m.Rooms){
		if entry.Occupancy_topic == topic{
			 return OCCUPANCY
		}
		for _, mt := range(entry.Motion_topics){
			if mt == topic{
				return MOTION
			}
		}
		for _, pt := range(entry.Pic_topics){
			if pt == topic{
				return PIC
			}
		}
		for _, dt := range(entry.Door_topics){
			if dt == topic{
				return DOOR
			}
		}
	}
	return -1
}

func (m Model) FindOccupancyTopicByRoom(room string)(string){
	for _, entry := range(m.Rooms){
		if entry.Name == room {
			return entry.Occupancy_topic
		}
	}
	return ""
}

func (m Model) ModelStatus() *ModelStatus{
	return model_status
}

func (m *Model) BuildModel() error{
	if model_status == nil {
		model_status = newModelStatus()
	}
	err := Config.UnmarshalKey("model",m)
	if err != nil {
		Logger.Error().Msgf("error unmarshaling model: %v", err)
		return fmt.Errorf("error")
	}
	return nil
}

func (m *Model) RoomOccupancyPeriod(room string) int64{
	for _, entry := range(m.Rooms){
		if entry.Name == room {
			return entry.Occupancy_period
		}
	}
	return Config.GetInt64("occupancy_period_default")
}

func (m Model) SubscribeTopics() []string {
	var topics []string
	for _, room := range(m.Rooms){
		topics = append(topics,room.Motion_topics...)
		topics = append(topics,room.Pic_topics...)
		topics = append(topics,room.Door_topics...)
	}
	return topics
}