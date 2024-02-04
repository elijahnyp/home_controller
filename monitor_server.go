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
			logger.Warn().Msgf("Problem loading monitor server: %v", err)
		}
		logger.Debug().Msg("monitor server shutdown")
		s.running.Unlock()
	}()
	return nil
}

func (s *MonitorServer)AddHandler(path string, handler func(http.ResponseWriter, *http.Request)){
	http.HandleFunc(path, handler)
	// s.handlers[path] = handler
}

func (s *MonitorServer)AddRawHandler(path string, handler http.Handler){
	http.Handle(path, handler)

}

// func (s MonitorServer)RemoveHandler(path string){
// 	delete(s.handlers, path)
// }

func (s *MonitorServer)Restart(){
	logger.Debug().Msg("restarting monitor server")
	if ! s.running.TryLock() { //only shutdown if not running
		logger.Debug().Msg("monitor server running, shutting it down")
		s.srv.Shutdown(context.TODO())
	} else {
		s.running.Unlock()
	}
	logger.Debug().Msg("waiting for shutdown")
	s.running.Lock() //when server shuts down it will unlock, so wait for unlock
	logger.Debug().Msg("http not running - good for startup")
	s.running.Unlock()
	s.Start()
}