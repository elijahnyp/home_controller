package util

import (
	"sync"
	"time"
)

// Async MQTT publisher: callers enqueue messages and never block on the broker.
// A small worker pool delivers them with a bounded wait and retry/backoff, so a
// slow or briefly disconnected broker can't stall the occupancy pipeline.

type publishJob struct {
	topic    string
	payload  []byte
	qos      byte
	retained bool
}

const (
	publishQueueSize   = 256
	publishWorkers     = 4
	publishWaitTimeout = 5 * time.Second
	publishMaxAttempts = 3
	publishBaseBackoff = 100 * time.Millisecond
)

var (
	publishQueue chan publishJob
	publishOnce  sync.Once
)

// StartPublisher launches the publish worker pool. Safe to call more than once.
func StartPublisher() {
	publishOnce.Do(func() {
		publishQueue = make(chan publishJob, publishQueueSize)
		for i := 0; i < publishWorkers; i++ {
			go publishWorker()
		}
	})
}

// PublishAsync enqueues a message for delivery with retry. It never blocks on
// the broker; if the queue is full the message is dropped and counted.
func PublishAsync(topic string, qos byte, retained bool, payload []byte) {
	if publishQueue == nil {
		Logger.Warn().Msgf("publisher not started; dropping message for %s", topic)
		return
	}
	select {
	case publishQueue <- publishJob{topic: topic, payload: payload, qos: qos, retained: retained}:
	default:
		Logger.Warn().Msgf("publish queue full; dropping message for %s", topic)
		RecordPublish("dropped", 0)
	}
}

func publishWorker() {
	for job := range publishQueue {
		deliver(job)
	}
}

func deliver(job publishJob) {
	backoff := publishBaseBackoff
	for attempt := 1; attempt <= publishMaxAttempts; attempt++ {
		if Client == nil || !Client.IsConnected() {
			Logger.Debug().Msgf("publish skipped (client not connected) for %s (attempt %d)", job.topic, attempt)
		} else {
			start := time.Now()
			token := Client.Publish(job.topic, job.qos, job.retained, job.payload)
			if token.WaitTimeout(publishWaitTimeout) {
				if token.Error() == nil {
					RecordPublish("ok", time.Since(start))
					return
				}
				Logger.Warn().Msgf("publish error for %s (attempt %d): %v", job.topic, attempt, token.Error())
				RecordPublish("error", time.Since(start))
			} else {
				Logger.Warn().Msgf("publish timeout for %s (attempt %d)", job.topic, attempt)
				RecordPublish("timeout", time.Since(start))
			}
		}
		if attempt < publishMaxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	Logger.Error().Msgf("publish failed after %d attempts for %s", publishMaxAttempts, job.topic)
}
