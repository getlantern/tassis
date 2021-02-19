package serviceimpl

import (
	"github.com/golang/protobuf/proto"

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
