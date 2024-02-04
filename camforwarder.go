package main

// TODO  THIS DOES NOT HANDLE CHANGES IN CONFIG YET

import (
	"net/http"
	// MQTT "github.com/eclipse/paho.mqtt.golang"
	"io"
	"time"
)

var queue chan CamForwarderCamera
var cam_forwarder CamForwarder
var ticker *time.Ticker

type CamForwarder struct {
	Enabled bool `mapstructure:"enabled"`
	Cameras []CamForwarderCamera `mapstructure:"cameras"`
	Frequency int64 `mapstructure:"frequency"`
	Workers int64 `mapstructure:"workers"`
}

type CamForwarderCamera struct {
	Url string `mapstructure:"snap_url"`
	Topic string `mapstructure:"topic"`
}

func (cf *CamForwarder)MakeCamForwarder() {
	err := Config.UnmarshalKey("cam_forwarder", cf)
	if err != nil {
		logger.Error().Msgf("Error loading cam_forwarder config: %v",err)
	}
	if queue == nil {
		queue = make(chan CamForwarderCamera, cf.Workers * 4)
	}
	for i := 0; i < int(cf.Workers); i++ {
		go cam_worker(queue)
	}
}

func (forwarder *CamForwarder) Start(){
	ticker = time.NewTicker(time.Duration(forwarder.Frequency) * time.Second)
	go func() {
		for {
			<-ticker.C
			for _,c := range forwarder.Cameras{
				queue <- c
			}
		}
	}()
}

func cam_worker(jobs <-chan CamForwarderCamera){
	for job := range jobs{
		process_job(job)
	}
}

func process_job(job CamForwarderCamera){
	req, err := http.NewRequest("GET", job.Url, nil)
	if err != nil {
		logger.Warn().Msgf("Unable to get pic from %v: %v",job.Url,err.Error())
		return
	}
	req.Header.Set("Accept", "*/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Warn().Msgf("Unable to get pic from %v: %v",job.Url,err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode > 299 || resp.StatusCode < 200 {
		logger.Warn().Msgf("non-2xx code received from camera: %d",resp.StatusCode)
		return
	}
	if resp.Header.Get("Content-Type") != "image/jpeg" {
		logger.Warn().Msgf("Invalid image mimetype for %v: %v",job.Url,resp.Header.Get("Content-Type"))
		// io.ReadAll(resp.Body)
		return
	}
	img, err := io.ReadAll(resp.Body)
	token := client.Publish(job.Topic, byte(0), false, img)
	token.Wait()
}