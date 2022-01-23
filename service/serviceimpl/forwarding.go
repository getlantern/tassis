package serviceimpl

import (
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/getlantern/errors"

	"github.com/getlantern/libmessaging-go/identity"
	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/model"
)

const (
	forwardingTopic      = "forwarding"
	forwardingRetryTopic = "forwardingretry"
)

func (srvc *Service) startForwarding() error {
	subscriber, err := srvc.broker.NewSubscriber(forwardingTopic)
	if err != nil {
		return err
	}

	retrySubscriber, err := srvc.broker.NewSubscriber(forwardingRetryTopic)
	if err != nil {
		return err
	}

	retryPublisher, err := srvc.publisherFor(forwardingRetryTopic)
	if err != nil {
		return err
	}

	for i := 0; i < srvc.forwardingParallelism; i++ {
		go srvc.forwardMessages(subscriber, retryPublisher)
		go srvc.forwardMessages(retrySubscriber, retryPublisher)
	}

	return nil
}

func (srvc *Service) forwardMessages(subscriber broker.Subscriber, retryPublisher broker.Publisher) {
	for i := 0; i < srvc.forwardingParallelism; i++ {
		go func() {
			for brokerMsg := range subscriber.Messages() {
				err := srvc.forwardMessage(subscriber, retryPublisher, brokerMsg)
				if err != nil {
					log.Error(err)
				}
			}
		}()
	}
}

func (srvc *Service) forwardMessage(subscriber broker.Subscriber, retryPublisher broker.Publisher, brokerMsg broker.Message) error {
	ack := brokerMsg.Acker()
	msg := &model.ForwardedMessage{}
	err := proto.Unmarshal(brokerMsg.Data(), msg)
	if err != nil {
		ack()
		return errors.New("unable to unmarshal ForwardedMessage: %v", err)
	}
	if msg.LastFailed != 0 {
		durationSinceLastFailure := msg.DurationSinceLastFailure()
		if durationSinceLastFailure < srvc.minForwardingRetryInterval {
			// delay to avoid tight loop on retries
			// new messages are processed on a separate set of goroutines, so this won't block them
			time.Sleep(srvc.minForwardingRetryInterval - durationSinceLastFailure)
		}
	}

	failMessage := func() {
		msg.MarkFailed()
		if msg.HasBeenFailingFor() > srvc.forwardingTimeout {
			// discard message that's been failing for too long
			ack()
			return
		}
		msgBytes, err := proto.Marshal(msg)
		if err != nil {
			log.Errorf("unable to marshal ForwardedMessage: %v", err)
			ack()
			return
		}
		err = retryPublisher.Publish(msgBytes)
		if err == nil {
			ack()
		} else {
			// this is an unusual case. don't ack so that message is not lost
			// note - this will lead to the forwarding queue potentially getting quite large
			// TODO: make sure we don't accidentally retransmit a boatload of messages
		}
	}

	tassisHost, err := srvc.presenceRepo.Find(msg.GetMessage().To)
	if err != nil {
		failMessage()
		return errors.New("unable to find tassis host: %v", err)
	}

	if tassisHost == srvc.publicAddr {
		// user came back to us, just publish locally
		topic := topicForAddr(msg.GetMessage().To)
		publisher, err := srvc.publisherFor(topic)
		if err != nil {
			failMessage()
			return errors.New("unable to get publisher: %v", err)
		}
		err = publisher.Publish(msg.GetMessage().GetUnidentifiedSenderMessage())
		if err != nil {
			failMessage()
			return errors.New("unable to publish: %v", err)
		}
		ack()
		return nil
	}

	srvc.forwarder.Forward(msg, tassisHost, func(forwardingErr error) {
		if forwardingErr != nil {
			failMessage()
		} else {
			ack()
		}
	})

	return nil
}

func (srvc *Service) startUserTransfers() error {
	publisher, err := srvc.publisherFor(forwardingTopic)
	if err != nil {
		return err
	}

	go func() {
		for {
			time.Sleep(srvc.userTransferInterval)
			addrs, err := srvc.db.AllRegisteredDevices()
			if err != nil {
				log.Errorf("unable to list registered devices: %v", err)
				continue
			}
			for _, addr := range addrs {
				tassisHost, err := srvc.presenceRepo.Find(addr)
				if err != nil {
					log.Error(err)
					continue
				}
				if tassisHost != srvc.publicAddr {
					// device moved to a different tassis, transfer messages
					topic := topicFor(identity.PublicKey(addr.IdentityKey), addr.DeviceId)
					subscriber, err := srvc.broker.NewSubscriber(topic)
					if err != nil {
						continue
					}
					err = srvc.transferDeviceMessages(subscriber, addr, publisher)
					if err != nil {
						log.Error(err)
					} else {
						srvc.db.Unregister(identity.PublicKey(addr.IdentityKey), addr.DeviceId)
						// TODO: make sure we don't have any weird race conditions here if new messages come in after we did the transfer
					}
				}
			}
		}
	}()

	return nil
}

func (srvc *Service) transferDeviceMessages(subscriber broker.Subscriber, addr *model.Address, publisher broker.Publisher) error {
	defer subscriber.Close()

	for {
		select {
		case brokerMsg := <-subscriber.Messages():
			forwardedMsg := &model.ForwardedMessage{
				Message: &model.OutboundMessage{
					To:                        addr,
					UnidentifiedSenderMessage: brokerMsg.Data(),
				},
			}
			forwardedMsgBytes, err := proto.Marshal(forwardedMsg)
			if err != nil {
				return errors.New("unable to marshal message for transfer: %v", err)
			}
			err = publisher.Publish(forwardedMsgBytes)
			if err != nil {
				return errors.New("unable to publish message for transfer: %v", err)
			}
			brokerMsg.Acker()()
		case <-time.After(srvc.userTransferInterval / 10): // TODO: make this tunable
			// done
			return nil
		}
	}
}
