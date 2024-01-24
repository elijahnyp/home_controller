package main

import (
	"net/http"
	"context"
	"sync"
	"fmt"
)

type MonitorServer struct {
	srv *http.Server
	// handlers map[string]func(http.ResponseWriter, *http.Request)
	running *sync.Mutex
}

func NewMonitorServer() *MonitorServer{
	var s MonitorServer
	// s.handlers = make(map[string]func(http.ResponseWriter, *http.Request))
	s.running = &sync.Mutex{}
	s.srv = &http.Server{}
	return &s
}

func (s *MonitorServer)Start() error{
	if ! s.running.TryLock(){
		return fmt.Errorf("already running")
	} else {
		s.running.Unlock()
	}
	// for path, handler := range(s.handlers){
	// 	http.HandleFunc(path, handler)
	// }
	go func() {
		s.running.Lock()
		s.srv = &http.Server{Addr: fmt.Sprintf(":%d",Config.GetInt("details_port"))}
		if err := s.srv.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("Problem loading monitor server: %v\n", err)
		}
		fmt.Println("monitor server shutdown")
		s.running.Unlock()
	}()
	return nil
}

func (s *MonitorServer)AddHandler(path string, handler func(http.ResponseWriter, *http.Request)){
	http.HandleFunc(path, handler)
	// s.handlers[path] = handler
}

// func (s MonitorServer)RemoveHandler(path string){
// 	delete(s.handlers, path)
// }

func (s *MonitorServer)Restart(){
	fmt.Println("restarting monitor server")
	if ! s.running.TryLock() { //only shutdown if not running
		fmt.Println("monitor server running, shutting it down")
		s.srv.Shutdown(context.TODO())
	} else {
		s.running.Unlock()
	}
	fmt.Println("waiting for shutdown")
	s.running.Lock() //when server shuts down it will unlock, so wait for unlock
	fmt.Println("http not running - good for startup")
	s.running.Unlock()
	s.Start()
}