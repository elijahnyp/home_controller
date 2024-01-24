package main

import "fmt"

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
}

type ModelStatus struct {
	room_status map[string]RoomStatus
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
		fmt.Println(err)
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