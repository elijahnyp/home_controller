package state

type Room interface {
	Status() RoomState
	SetState(RoomState) error
}

type RoomState struct {
	Lights []Light
	Sensors []Sensor
	Occupied bool
}