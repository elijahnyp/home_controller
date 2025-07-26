package util

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

type MonitorServer struct {
	running *sync.Mutex
	srv     *http.Server
	srvMu   sync.RWMutex // protects srv field
}

func NewMonitorServer() *MonitorServer {
	var s MonitorServer
	s.running = &sync.Mutex{}
	s.srv = &http.Server{}
	return &s
}

func (s *MonitorServer) Start() error {
	if !s.running.TryLock() {
		return fmt.Errorf("already running")
	} else {
		s.running.Unlock()
	}
	go func() {
		s.running.Lock()

		// Create new server with proper synchronization
		newSrv := &http.Server{Addr: fmt.Sprintf(":%d", Config.GetInt("details_port"))}
		s.srvMu.Lock()
		s.srv = newSrv
		s.srvMu.Unlock()

		if err := newSrv.ListenAndServe(); err != http.ErrServerClosed {
			Logger.Warn().Msgf("Problem loading monitor server: %v", err)
		}
		Logger.Debug().Msg("monitor server shutdown")
		s.running.Unlock()
	}()
	return nil
}

func (s *MonitorServer) AddHandler(path string, handler func(http.ResponseWriter, *http.Request)) {
	http.HandleFunc(path, handler)
}

func (s *MonitorServer) AddRawHandler(path string, handler http.Handler) {
	http.Handle(path, handler)

}

// func (s MonitorServer)RemoveHandler(path string){
// 	delete(s.handlers, path)
// }

func (s *MonitorServer) Restart() {
	Logger.Debug().Msg("restarting monitor server")
	if !s.running.TryLock() { // only shutdown if not running
		Logger.Debug().Msg("monitor server running, shutting it down")

		// Safely access srv with read lock
		s.srvMu.RLock()
		currentSrv := s.srv
		s.srvMu.RUnlock()

		if currentSrv != nil {
			if err := currentSrv.Shutdown(context.TODO()); err != nil {
				Logger.Error().Msgf("Error shutting down monitor server: %v", err)
			}
		}
	} else {
		s.running.Unlock()
	}
	Logger.Debug().Msg("waiting for shutdown")
	s.running.Lock() // when server shuts down it will unlock, so wait for unlock
	Logger.Debug().Msg("http not running - good for startup")
	s.running.Unlock()
	if err := s.Start(); err != nil {
		Logger.Error().Msgf("Error starting monitor server: %v", err)
	}
}
