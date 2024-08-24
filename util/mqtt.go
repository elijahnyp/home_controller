package util

import (
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"fmt"
)

var Client MQTT.Client

var subscriptions map[string]MQTT.MessageHandler

var connectHandler MQTT.OnConnectHandler = func(client MQTT.Client) {
    Logger.Info().Msg("Connected")
	subscribe()
	// for _, topic := range model.SubscribeTopics() {
	// 	if token := client.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
	// 		Logger.Panic().Msgf("Error Subscribing: %v",fmt.Errorf("%v", token.Error()))
	// 		// os.Exit(1)
	// 	}
	// }
}

func subscribe(){
	if subscriptions == nil {
		subscriptions = make(map[string]MQTT.MessageHandler)
	}
	for topic, handler := range subscriptions{
		if token := Client.Subscribe(topic, 0, handler); token.Wait() && token.Error() != nil {
			Logger.Panic().Msgf("Error Subscribing: %v",fmt.Errorf("%v", token.Error()))
		}
	}
}

func RegisterMQTTSubscription(topic string, handler MQTT.MessageHandler){
	if subscriptions == nil {
		subscriptions = make(map[string]MQTT.MessageHandler)
	}
	if handler == nil{
		delete(subscriptions, topic)
	} else {
		subscriptions[topic] = handler
	}
}

func receiver(client MQTT.Client, message MQTT.Message){
	Logger.Warn().Msgf("Received message on %v but no handler",message.Topic())
}

var connectLostHandler MQTT.ConnectionLostHandler = func(client MQTT.Client, err error) {
    Logger.Info().Msgf("Connect lost: %v", err)
}

func MqttInit(){
	opts := MQTT.NewClientOptions()
	opts.AddBroker(Config.GetString("broker_uri"))
	opts.SetClientID(Config.GetString("id_base") + "_" + GetRandString((6)))
	opts.SetUsername(Config.GetString("username"))
	opts.SetPassword(Config.GetString("password"))
	opts.SetCleanSession(Config.GetBool("cleansess"))
	opts.SetAutoReconnect(true)
	opts.OnConnectionLost = connectLostHandler
	opts.OnConnect = connectHandler

	opts.SetDefaultPublishHandler(receiver)

	if Client != nil {
		Logger.Debug().Msg("Client exists - destroying")
		if Client.IsConnected() {
			Client.Disconnect(1000)
		}
		Client = nil
	}

	Client = MQTT.NewClient(opts)

	if token := Client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	// for _, topic := range Config.GetStringSlice("topics") {

}