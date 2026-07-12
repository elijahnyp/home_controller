package util

import (
	"testing"
	"time"
)

func TestPublishAsyncDelivers(t *testing.T) {
	mock := &MockMQTTClient{connected: true}
	Client = mock

	StartPublisher()
	PublishAsync("pub/test", 0, false, []byte("hello"))

	// Delivery happens on a worker goroutine; poll for it.
	delivered := false
	for i := 0; i < 100; i++ {
		mock.mu.RLock()
		n := len(mock.publishCalls)
		mock.mu.RUnlock()
		if n >= 1 {
			delivered = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !delivered {
		t.Fatal("expected PublishAsync to deliver a message via the worker")
	}

	mock.mu.RLock()
	call := mock.publishCalls[0]
	mock.mu.RUnlock()
	if call.Topic != "pub/test" {
		t.Errorf("published to %s, expected pub/test", call.Topic)
	}
	if payload, ok := call.Payload.([]byte); !ok || string(payload) != "hello" {
		t.Errorf("published payload %v, expected 'hello'", call.Payload)
	}
}

func TestPublishAsyncNilClientNoPanic(t *testing.T) {
	// A disconnected client must not panic; deliver() retries then gives up.
	mock := &MockMQTTClient{connected: false}
	Client = mock
	StartPublisher()
	PublishAsync("pub/down", 0, false, []byte("x"))
	// Give the worker a moment to run its retry loop.
	time.Sleep(50 * time.Millisecond)
}
