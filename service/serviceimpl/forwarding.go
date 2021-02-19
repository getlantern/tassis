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
	forwardingTopic = "forwarding"
)

func (srvc *Service) startForwarding() error {
	subscriber, err := srvc.broker.NewSubscriber(forwardingTopic)
	if err != nil {
		return err
	}

	republisher, err := srvc.publisherFor(forwardingTopic)
	if err != nil {
		return err
	}

	for i := 0; i < srvc.forwardingParallelism; i++ {
		go func() {
			for brokerMsg := range subscriber.Messages() {
				ack := brokerMsg.Acker()
				msg := &model.ForwardedMessage{}
				err := proto.Unmarshal(brokerMsg.Data(), msg)
				if err != nil {
					log.Errorf("unable to unmarshal ForwardedMessage: %v", err)
					ack()
					continue
				}

				srvc.forwarder.Forward(msg, func(forwardingErr error) {
					if forwardingErr == nil {
						ack()
					} else {
						msg.MarkFailed()
						if msg.HasBeenFailingFor() > srvc.forwardingTimeout {
							log.Debugf("disposing of message that has been failing for longer than %v", srvc.forwardingTimeout)
							ack()
						} else {
							msgBytes, err := proto.Marshal(msg)
							if err != nil {
								log.Errorf("unable to marshal ForwardedMessage: %v", err)
								ack()
								return
							}
							err = republisher.Publish(msgBytes)
							if err == nil {
								ack()
							} else {
								// this is an unusual case. don't ack so that message is not lost
								// note - this will lead to the forwarding queue potentially getting quite large
								// TODO: make sure we don't accidentally retransmit a boatload of messages
							}
						}
					}
				})
			}
		}()
	}

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
				host, err := srvc.presenceRepo.Find(addr)
				if err != nil {
					log.Error(err)
					continue
				}
				if host != srvc.publicAddr {
					// device moved to a different tassis, transfer messages
					topic := topicFor(identity.UserID(addr.UserID), addr.DeviceID)
					subscriber, err := srvc.broker.NewSubscriber(topic)
					if err != nil {
						continue
					}
					err = srvc.transferDeviceMessages(subscriber, addr, host, publisher)
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

func (srvc *Service) transferDeviceMessages(subscriber broker.Subscriber, addr *model.Address, host string, publisher broker.Publisher) error {
	defer subscriber.Close()

	for {
		select {
		case brokerMsg := <-subscriber.Messages():
			forwardedMsg := &model.ForwardedMessage{
				Message: &model.OutboundMessage{
					To:                        addr,
					UnidentifiedSenderMessage: brokerMsg.Data(),
				},
				ForwardTo: host,
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
