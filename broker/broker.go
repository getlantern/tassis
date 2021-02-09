package broker

type Message interface {
	Data() []byte

	Ack()
}

type Subscriber interface {
	Channel() <-chan Message

	Close()
}

type Publisher interface {
	Publish(msg []byte)

	Close()
}

type Broker interface {
	NewSubscriber(topic string) Subscriber

	NewPublisher(topic string) Publisher
}
