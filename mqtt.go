package main

import (
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"fmt"
)

var client MQTT.Client

var connectHandler MQTT.OnConnectHandler = func(client MQTT.Client) {
    logger.Debug().Msg("Connected")
	for _, topic := range model.SubscribeTopics() {
		if token := client.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
			logger.Panic().Msgf("Error Subscribing: %v",fmt.Errorf("%v", token.Error()))
			// os.Exit(1)
		}
	}
}

var connectLostHandler MQTT.ConnectionLostHandler = func(client MQTT.Client, err error) {
    logger.Debug().Msgf("Connect lost: %v", err)
}

func receiver(client MQTT.Client, message MQTT.Message) {
	logger.Debug().Msgf("Message Received on topic %s",message.Topic())
	var mitem MQTT_Item
	mitem.Data = message.Payload()
	mitem.Topic = message.Topic()
	mitem.Room = model.FindRoomByTopic(message.Topic())
	switch model.FindTopicType(message.Topic()) {
	case PIC:
		mitem.Type = PIC
		image_channel <- mitem
	case MOTION:
		mitem.Type = MOTION
		motion_channel <- mitem
		//do something here
	case OCCUPANCY:
		mitem.Type = OCCUPANCY
		//do something here
	default:
		logger.Debug().Msgf("topic %s not found in model.  Fix subscription or add to model", message.Topic())
	}
}

func MqttInit(){
	opts := MQTT.NewClientOptions()
	opts.AddBroker(Config.GetString("broker_uri"))
	opts.SetClientID(Config.GetString("id"))
	opts.SetUsername(Config.GetString("username"))
	opts.SetPassword(Config.GetString("password"))
	opts.SetCleanSession(Config.GetBool("cleansess"))
	opts.SetAutoReconnect(true)
	opts.OnConnectionLost = connectLostHandler
	opts.OnConnect = connectHandler

	opts.SetDefaultPublishHandler(receiver)

	if client != nil {
		logger.Debug().Msg("Client exists - destroying")
		if client.IsConnected() {
			client.Disconnect(1000)
		}
		client = nil
	}

	client = MQTT.NewClient(opts)

	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	// for _, topic := range Config.GetStringSlice("topics") {

}