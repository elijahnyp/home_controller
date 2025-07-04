package main

import (
	"text/template"
	"os"
	"fmt"
)

type TData struct {
	Name      string
	Mqtthost  string
	Mqttpass  string
	Mqttuser  string
	Mqttport  int
	Locations []Location
	Beacons []Beacon
}

type Location struct {
	Name   string
	Lat    float64
	Lon    float64
	Radius int
	Tst    int32
}

type Beacon struct {
	Name string
	Uuid string
	Tst int32
}

func main(){
	data := TData{
		Name: "elijah",
		Mqtthost: "mqtt.wangwood.house",
		Mqttport: 8883,
		Mqttuser: "elijah",
		Mqttpass: "1Ntp4mbh",
		Locations: []Location{{
				Name: "The Shack",
				Lat: 42.4247,
				Lon: -71.4539,
				Radius: 100,
				Tst: 1592531842,
			},{
				Name: "Wangwood Cottage",
				Lat: 42.531959,
				Lon: -71.531564,
				Radius: 125,
				Tst: 1592531842,
			},
		},
		Beacons: []Beacon{{
				Name: "Lexus",
				Uuid: "C9D1EF94-255B-447A-BA60-6ADBC13333F6",
				Tst: 1592531842,
			},{
				Name: "BMW",
				Uuid: "05ED9D3C-3C82-43B6-956B-A3964EC9946B",
				Tst: 1592531842,
			},
		},
	}
	fmt.Println(data)
	t, _ := template.ParseFiles("./owntracks_template.json.template")
	t.Option("missingkey=zero")
	fmt.Println(t)
	e :=t.Execute(os.Stdout, data)
	fmt.Printf(e.Error())

	// if err != nil {
	// 	fmt.Println("template results")
	// 	t.Execute(os.Stdout, data)
	// } else {fmt.Println(err.Error())}
}