package main

import (
	"sync"
	"sync/atomic"

	. "github.com/elijahnyp/home_controller/util"
)

// This file centralizes concurrent access to state shared between the MQTT
// pipeline goroutines and the HTTP/websocket/metric readers. Each map is guarded
// by its own mutex, and the immutable-after-build model config is swapped
// atomically on reload so readers never take a lock.

// ---- model config (swapped atomically on config reload) --------------------

var modelPtr atomic.Pointer[Model]

// CurrentModel returns the active model config. The returned pointer must be
// treated as read-only; config reloads publish a brand-new Model via SetModel.
func CurrentModel() *Model {
	if m := modelPtr.Load(); m != nil {
		return m
	}
	return &Model{}
}

// SetModel atomically publishes a freshly built model config.
func SetModel(m *Model) {
	modelPtr.Store(m)
}

// ---- image/detection cache -------------------------------------------------

var (
	cacheMu sync.RWMutex
	cache   = make(map[string]ImageCacheItem)
)

func CacheSet(topic string, item ImageCacheItem) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[topic] = item
}

func CacheGet(topic string) (ImageCacheItem, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	item, ok := cache[topic]
	return item, ok
}

// ---- web-facing occupancy/motion state -------------------------------------

var (
	webStateMu           sync.RWMutex
	last_occupancy_state = make(map[string]bool)
	last_motion_state    = make(map[string]bool)
)

func SetOccupancyState(room string, occupied bool) {
	webStateMu.Lock()
	defer webStateMu.Unlock()
	last_occupancy_state[room] = occupied
}

func GetOccupancyState(room string) (bool, bool) {
	webStateMu.RLock()
	defer webStateMu.RUnlock()
	v, ok := last_occupancy_state[room]
	return v, ok
}

func SetMotionState(topic string, on bool) {
	webStateMu.Lock()
	defer webStateMu.Unlock()
	last_motion_state[topic] = on
}

func GetMotionState(topic string) bool {
	webStateMu.RLock()
	defer webStateMu.RUnlock()
	return last_motion_state[topic]
}
