package state

type Device interface {
	Status() uint
	State(uint) error
	Location() string
	SetConfig(any) error
	GetConfig() any
}
