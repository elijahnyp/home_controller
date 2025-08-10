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

## configuration
The occupancy configuration is goverened by a json file.  A sample is in home_controller.json
* broker_url: mqtt server address
* id: client id for mqtt
* log_level: internal logging level
* frequency: minimum time (in seconds) between submitting images from the same camera to the ai server
* occupied_period_default: default occupied_period for rooms if the room doesn't specify a different value
* insecure_tls: enable/disable tls verification in both mqtt and web requests
* detection_url: url to the ai server
* details_port: port a local webserver runs on that provides system status and an API
* model: object representing rooms to monitor
  * rooms: array of rooms to monitor.  Each entry is a 'room'
    * name: name of room
    * occupancy_period: occupancy timer length in seconds
    * occupancy_topic: topic to publish occupancy decisions
    * motion_topics: mqtt topics to monitor for motion events
    * door_topics: mqtt topics to monitor for door open/close events
    * pic_topics: mqtt topics to monitor for pictures of the room
* cam_forwarder: configuration object for cam_forwarder that retrieves images from http servers and forwards them into mqtt
  * enabled: enables/disables the cam_forwarder
  * frequency: how often to retrieve images from the URLs
  * workers: number of goroutines to spawn to monitor images
  * cameras: array of snap_url/topic pairs to monitor for images and forward to topics

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