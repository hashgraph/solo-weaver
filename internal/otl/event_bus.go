package otl

import (
	ebus "github.com/asaskevich/EventBus"
)

const (
	// TopicConnected is for connected events when notel is able to connect to the OTel collector
	TopicConnected = "notel:connected"

	// TopicDisconnected is for disconnected events when notel is unable to connect to the OTel collector
	TopicDisconnected = "notel:disconnected"
)

// eventBus is the global event but for notel package
var eventBus = ebus.New()

// SubscribeOnce subscribes the handler to a notel topic  for once
// handler will be removed after the first event
func SubscribeOnce(topic string, handler interface{}) error {
	return eventBus.SubscribeOnce(topic, handler)
}

// Subscribe subscribes the handler to a notel topic
func Subscribe(topic string, handler interface{}) error {
	return eventBus.Subscribe(topic, handler)
}
