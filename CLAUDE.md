# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# Commands
* build: `go build ./...` (produces the `home_controller` binary from the `main` package at repo root)
* run: `go build . && ./home_controller` — reads `home_controller.json` from `.`, `./config`, `/`, `/etc`, or `/home_controller` (see `SetupConfig`). Copy `home_controller.sample.json` to `home_controller.json` to start.
* test all: `go test -race ./...`
* single package: `go test -race ./util/`
* single test: `go test -race -run TestName ./util/`
* coverage: `go test -race -coverprofile=coverage.out ./... && go tool cover -html=coverage.out`
* lint: `golangci-lint run --timeout=5m --config=.golangci.yml` (config pins linters/exclusions; CI uses golangci-lint v2.11.1)
* format (required before pushing): `go fmt ./...` — CI fails if `gofmt -s -l .` reports any file; also runs `go vet ./...`
* docker: `docker build -t home_controller .` (multi-stage; also built multi-arch for arm64/amd64/arm/v7 in CI)

# Architecture

## Big picture
Single Go binary. `main` package lives at the repo root (`occupancy.go`, `web_handlers.go`); the `util/` package holds all reusable infrastructure and is imported with a dot-import (`. "github.com/elijahnyp/home_controller/util"`), so `Config`, `Logger`, `Client`, `Model`, etc. are referenced unqualified in root files.

The system is an MQTT-driven pipeline. `receiver` (in `occupancy.go`) is the single MQTT message handler; it classifies each message by topic via `model.FindTopicType` and fans it out onto one of four buffered channels. Dedicated goroutines (started in `Init`) consume those channels:
* `image_channel` → `ProcessImageRoutine` → throttles per-topic by `frequency`, then spawns `ProcessImage` which calls Triton and pushes an OCCUPIED/UNOCCUPIED verdict onto `results_channel`.
* `motion_channel` → `MotionManagerRoutine` → parses motion/door payloads (`0`/`ON`/`OFF`/`OPEN`/`CLOSED`) into MOTION_START/MOTION_STOP and forwards to `results_channel`.
* `results_channel` → `OccupancyManagerRoutine` → the core occupancy state machine (see below). Publishes `"true"`/`"false"` to each room's `occupancy_topic`.
* `door_channel` → currently drained but not processed (door logic folded into motion handling).

## Occupancy state machine
Implemented in `OccupancyManagerRoutine` (`occupancy.go`) using `RoomStatus` (`util/model.go`). Rules: motion, a detected `person`, or a **door opening** (`DOOR_OPEN`) marks the room occupied and resets `last_occupied`. A room only becomes unoccupied when the camera opinion has expired (`last_occupied < now - occupancy_period`) AND motion is off. Per-room `occupancy_period` falls back to `occupancy_period_default`. Message-type and analysis-result constants (`PIC`/`MOTION`/`OCCUPANCY`/`DOOR`, `OCCUPIED`/`UNOCCUPIED`/`MOTION_START`/`DOOR_OPEN`/…) are `iota` blocks in `util/model.go`.

Door events are consumed by `DoorManagerRoutine` (dedicated `door_topics`) and, for door sensors filed under `motion_topics`, by `MotionManagerRoutine`: an opening (`OPEN`/`ON`/`1`/`true`) emits `DOOR_OPEN`; a closing is a deliberate no-op (the cam/motion timers drive the room back to unoccupied).

## AI inference (Triton)
`util/triton_client.go` talks to a Triton Inference Server over gRPC (generated stubs in `triton/generated/`, protos in `triton/proto/`). `DetectObjects` does the full YOLO11 pipeline in Go: letterbox resize → NCHW float32 tensor → `ModelInfer` RPC → parse `[1, 4+numClasses, numAnchors]` output → per-class NMS → map COCO class indices to labels → rescale boxes to original image space. Results are cached per pic-topic in the `cache` map for the web/markup handlers. Note: the earlier REST `detection_url` backend was replaced by this gRPC client (commit 50537b4); older docs referencing `detection_url` are stale.

## Web / monitor server
`util/monitor_server.go` runs a single `http.ServeMux` (via `http.HandleFunc`) on `details_port`, restartable on config change. Handlers are registered in `main`:
* Legacy (keep intact): `/image` (marked-up JPEG with detection boxes drawn by `MarkupImage`), `/room`, `/room_status`, `/model`.
* New UI: `/` and `/room_detail` serve `web/static/*.html`; `/ws` is the websocket; `/api/status` and `/api/room` return JSON. Live updates are pushed through the `WSHub` (`web_handlers.go`) — `OccupancyManagerRoutine` calls `wsHub.BroadcastUpdate` on every state change.
* `/metrics` — Prometheus exposition (added via `AddRawHandler`), see Observability below.

## Concurrency & shared state
State shared between the pipeline goroutines and the HTTP/websocket/metric readers is centralized in `state_sync.go` (main) and guarded: the model **config** is swapped atomically on reload (`CurrentModel()` / `SetModel()`, `atomic.Pointer[Model]` — readers are lock-free); the detection `cache` and the web-facing occupancy/motion maps sit behind `sync.RWMutex` with `CacheGet/CacheSet`, `Get/SetOccupancyState`, `Get/SetMotionState`; runtime `RoomStatus` (in `util/model.go`) is guarded by `statusMu` with `GetRoomStatus`/`SnapshotRoomStatuses`. Never touch these maps directly — go through the accessors, or `go test -race` will (correctly) fail.

Image inference is bounded: `ProcessImageRoutine` is a single dispatcher (owns `last_processed` so it needs no lock) that hands work to a semaphore-bounded pool (`inferenceConcurrency`) instead of the old unbounded `go ProcessImage`.

## Async MQTT publisher
`util/publisher.go` — hot-path publishes (occupancy decisions, online ping) go through `PublishAsync`, which enqueues to a buffered channel drained by a small worker pool. Workers deliver with a bounded `WaitTimeout` and retry with exponential backoff; a full queue drops-and-counts rather than blocking the caller. `StartPublisher()` is called once in `Init()`. Cam-forwarder and HA advertisement still publish synchronously (per-frame / low-rate, and covered by synchronous tests). Background publishers/advertisers **log-and-continue** on error — no `panic` (an earlier `Logger.Panic` in `AdvertiseHA` and `panic` in `MqttInit` could crash the process).

## Observability (OpenTelemetry → Prometheus)
`util/metrics.go` — `InitMetrics(StateProviders)` builds an OTel meter provider with a Prometheus exporter and returns the `/metrics` handler; it also starts Go runtime metrics. Instruments live in an `atomic.Pointer[instruments]` and all `Record*` helpers are nil-safe no-ops until init (so tests need no metrics setup). Synchronous counters/histograms cover detection latency, message receive/publish, cam fetches, skips, config reloads, and domain results (`objects_detected_total`, `object_confidence{room,object}`, `person_detections_total`, `occupancy_transitions_total`). Current state (room occupied/motion/seconds-since, channel depths, ws clients, mqtt connected) is exposed via **observable gauges** whose callbacks read through the thread-safe accessors — `main` supplies those via `MetricProviders()` (`metrics_providers.go`). Exporter is configured `WithoutUnits`/`WithoutCounterSuffixes` so instrument names (already carrying `_total`/`_seconds`) are emitted verbatim.

## Config (viper)
`util/settings.go` uses viper with hot-reload: `Config.WatchConfig()` fires `OnNewConfig`, which runs every callback registered via `RegisterNewConfigListener` (rebuild model, re-subscribe topics, reinit Triton, restart monitor server, re-init MQTT). When editing config behavior, register a listener rather than reading config once at startup. Environment variables override file values (`AutomaticEnv`). **Actual config keys differ from some older prose docs** — authoritative keys are the `Config.SetDefault`/`GetString` calls: `broker_uri`, `id_base`, `username`, `password`, `cleansess`, `log_level`, `frequency`, `occupancy_period_default`, `min_confidence`, `insecure_tls`, `details_port`, `triton_url`/`triton_model`/`triton_input_*`/`triton_output_name`/`triton_iou_threshold`, `model.rooms[]`, `cam_forwarder`. See `home_controller.sample.json`.

## Other components
* `util/camforwarder.go` — worker-pool that polls HTTP snapshot URLs and republishes JPEGs into MQTT (for cameras that can't publish directly). Does not yet react to config changes.
* `util/ha.go` — publishes Home Assistant MQTT-discovery `binary_sensor` config for each room; re-advertised every 5 min (`HAAdvertiser`) and on connect. Nil-guards `Client`.
* `util/mqtt.go` — MQTT client lifecycle; subscriptions and connect-hooks are registered via `RegisterMQTTSubscription` / `RegisterMQTTConnectHook` and (re)applied on connect. `subscribeOccupancyTopics` (in `occupancy.go`) diffs desired vs. currently-subscribed topics on reload and **unsubscribes removed** ones.
* `state/` — an experimental `Room`/`Light`/`Sensor`/`Device` state abstraction with its own tests; **not currently wired into `main`**. Don't assume runtime behavior depends on it.

## Gotchas
* `main` runs `select {}` to block forever after wiring everything up; there is no graceful shutdown.
* `RoomStatus` is a value type — `GetRoomStatus` returns a copy; mutate the copy then write it back with `UpdateRoomStatus` (the occupancy goroutine is the only writer, so its read-modify-write stays serialized).

# this project specific
## intent
This project has 2 main functions:
* determining room occupancy
* forwarding images into MQTT from cameras that can't do it directly

Each room has an occupancy period, an mqtt topic to post status to, a list of topics to monitor for motion events in the room, a list of topics to monitor for door open/close events, and a list of mqtt topics to monitor for images from within the room.  The images are sent through an AI server to determine what objects are in the image whever a new image is received or at the global frequency, whichever longer.

These data points are combined into an occupancy decision using the following logic:
* if motion is active, the room is occupied
* if there is a person in any of the images of the room, the room is occupied
* if the room is unoccupied and the door opens, the room is occupied
* if both motion is false and no person is in any of the images, the room remains in the occupied state and a countdown timer is started.  The timer initial value is the occupancy_period and it's in seconds.  When it reaches zero the room becomes unoccupied.  If motion or a person is detected the countdown is reset
Occupancy decisions are emitted into MQTT

## interface
* there is a legacy interface which is minimal but should be left intact
* there should be a UI at the default URL showing the status of the rooms
  * this UI leverages websockets and should minimize client-side computation
  * it should update live as the status of the rooms changes
  * it each room should have a panel showing the current status of the room specifying if it's occupied, if the timer is running, if motion is detected, and how much time is remaining on the timer
  * each panel should be clickable to show details of the room.  This detail primarily consists of the marked-up images of the room showing where the detected objects are.  It should also include a list of the objects detected with the confidence level.

## ignore files
* config/owntracks_template.json.template

# Golang Projects
* use golangci-lint for security and style tests
* use latest stable version of go
* run go fmt before pushing

# Python Projects

# ESP32 Projects

# Global Preferences
* create tests
* use winget as first priority, then choco for softare management.  As a last resort download installer directly
* ask for confirmation before doing anything outside the project directory tree
* never change project files unless we're on a git branch.  Master and main should never be directly updated

# Global CI
* use github actions for CI/CD
  * if the project has a jenkins file, firmware images are build with jenkins but everything else is build with github actions
* container images are uploaded to ghcr
* prefer reusable github actions over raw commands
* configure dependabot to keep dependencies up to date
* use github cli
* never commit to main or master.  Do everything in a branch and open a PR
* helm charts are managed by argocd.  Don't use features of helm that argocd doesn't support
* securecodewarrior/gosec does not exist
* use conventional commits
