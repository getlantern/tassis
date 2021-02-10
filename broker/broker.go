package broker

// Message represents a message received from a Subscriber
type Message interface {
	// Data is the data contained in the message
	Data() []byte

	// Acker returns a function that can be used to acknowledge receipt of this message. Ideally, this function retains less memory than the full message
	Acker() func() error
}

// Subscriber represents a subscriber to a specific topic
type Subscriber interface {
	// Messages returns a channel from which one can read messages
	Messages() <-chan Message

	// Close closes the Subscriber, including closing the channel returned by Messages()
	Close()
}

// Publisher represents a publisher to a specific topic
type Publisher interface {
	// Publish publishes the given message data
	Publish(data []byte) error

	// Close closes the Publisher
	Close()
}

// Broker is an interface for a topic-based pub/sub broker. This broker should lazily create topics on first use by
// either subscribers or publishers.
type Broker interface {
	NewSubscriber(topicName string) (Subscriber, error)

	NewPublisher(topicName string) (Publisher, error)
}
