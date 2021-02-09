package service

import (
	"github.com/google/uuid"

	"github.com/getlantern/messaging-server/broker"
	"github.com/getlantern/messaging-server/message"
)

type Acknowledgable struct {
	brokerMessage broker.Message
}

func (ack *Acknowledgable) Message() message.Message {
	return message.Message(ack.brokerMessage.Data())
}

func (ack *Acknowledgable) Ack() {
	ack.brokerMessage.Ack()
}

type Service struct {
	Broker broker.Broker
}

// Connect connects a user to the service, returning channels for sending messages to the service and receiving messages from it.
// When the client wishes to disconnect, it should close the out channel.
func (service *Service) Connect(userID uuid.UUID, deviceID uint32) (out chan<- message.Message, in <-chan *Acknowledgable) {
	// TODO: authenticate user
	_out := make(chan message.Message)
	_in := make(chan *Acknowledgable)

	closeCh := make(chan interface{})
	go service.handleOutbound(userID, _out, closeCh)
	go service.handleInbound(userID, _in, closeCh)

	return _out, _in
}

func (service *Service) handleOutbound(userID uuid.UUID, out chan message.Message, closeCh chan interface{}) {
	defer close(closeCh)

	publisher := service.Broker.NewPublisher(userID.String())
	defer publisher.Close()

	for msg := range out {
		switch msg.Type() {
		case message.TypeRegister:

		}
	}
}

func (service *Service) handleInbound(userID uuid.UUID, in chan *Acknowledgable, closeCh chan interface{}) {
	subscriber := service.Broker.NewSubscriber(userID.String())
	defer subscriber.Close()

	ch := subscriber.Channel()
	for {
		select {
		case <-closeCh:
			return
		case msg := <-ch:
			in <- &Acknowledgable{msg}
		}
	}
}
