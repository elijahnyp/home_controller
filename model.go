package main

import (
	"fmt"
	"time"
)

var model_status *ModelStatus

type Model struct {
	Rooms []Room `mapstructure:"rooms"`
}

type Room struct {
	Name string `mapstructure:"name"`
	Occupancy_topic string `mapstructure:"occupancy_topic"`
	Motion_topics []string `mapstructure:"motion_topics"`
	Pic_topics []string `mapstructure:"pic_topics"`
	Occupancy_period int64 `mapstructure:"occupancy_period"`
}

type RoomStatus struct {
	last_occupied int64
	motion_state bool
	occupied bool
}

type ModelStatus struct {
	room_status map[string]RoomStatus
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

func NewModelStatus() *ModelStatus{
	s := ModelStatus{}
	s.room_status = make(map[string]RoomStatus)
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

func (m *Model) BuildModel() error{
	if model_status == nil {
		model_status = NewModelStatus()
	}
	err := Config.UnmarshalKey("model",m)
	if err != nil {
		logger.Error().Msgf("error unmarshaling model: %v", err)
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
	}
	return topics
}