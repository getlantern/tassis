package serviceimpl

import (
	"time"

	"github.com/golang/protobuf/proto"

	"github.com/getlantern/errors"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/identity"
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

	retryPublisher, err := srvc.publisherFor(forwardingTopic)
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
				ack := brokerMsg.Acker()
				msg := &model.ForwardedMessage{}
				err := proto.Unmarshal(brokerMsg.Data(), msg)
				if err != nil {
					log.Errorf("unable to unmarshal ForwardedMessage: %v", err)
					ack()
					return
				}
				durationSinceLastFailure := msg.DurationSinceLastFailure()
				if durationSinceLastFailure > 0 && durationSinceLastFailure < srvc.minForwardingRetryInterval {
					// delay to avoid tight loop on retries
					// new messages are processed on a separate set of goroutines, so this won't block them
					time.Sleep(srvc.minForwardingRetryInterval - durationSinceLastFailure)
				}

				failMessage := func() {
					msg.MarkFailed()
					if msg.HasBeenFailingFor() > srvc.forwardingTimeout {
						log.Debugf("discarding message that has been failing for longer than %v", srvc.forwardingTimeout)
						ack()
					} else {
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
				}

				tassisHost, err := srvc.presenceRepo.Find(msg.GetMessage().To)
				if err != nil {
					log.Errorf("unable to find tassis host: %v", err)
					failMessage()
					return
				}

				if tassisHost == srvc.publicAddr {
					// user came back to us, just publish locally
					topic := topicForAddr(msg.GetMessage().To)
					publisher, err := srvc.publisherFor(topic)
					if err != nil {
						log.Errorf("unable to get publisher: %v", err)
						failMessage()
						return
					}
					err = publisher.Publish(msg.GetMessage().GetUnidentifiedSenderMessage())
					if err != nil {
						log.Errorf("unable to publish: %v", err)
						failMessage()
						return
					}
					ack()
					return
				}

				srvc.forwarder.Forward(msg, tassisHost, func(forwardingErr error) {
					if forwardingErr != nil {
						failMessage()
					} else {
						ack()
					}
				})
			}
		}()
	}
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
					topic := topicFor(identity.UserID(addr.UserID), addr.DeviceID)
					subscriber, err := srvc.broker.NewSubscriber(topic)
					if err != nil {
						continue
					}
					err = srvc.transferDeviceMessages(subscriber, addr, publisher)
					if err != nil {
						log.Error(err)
					} else {
						srvc.db.Unregister(identity.UserID(addr.UserID), addr.DeviceID)
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
		case <-time.After(srvc.userTransferInterval / 100):
			// done
			return nil
		}
	}
}
