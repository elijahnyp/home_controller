# home_controller
home automation logic controllers written in go

Better README to follow

# architecture

* mqtt event bus
* components prep their structure on startup
  * store state as retained values in mqtt
* common state engine that each component maintains
  * configurable for topics to track

## drawbacks
* duplication of state across components

## benefits
* scalable
* simplified communication between components
* observable behavior between components