package rpc

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (c *Communicator) JoinTopic(topic string, opts ...pubsub.TopicOpt) (*pubsub.Topic, error) {
	return c.p2pSrv.JoinTopic(topic, opts...)
}

func (c *Communicator) LeaveTopic(topic string) error {
	return c.p2pSrv.LeaveTopic(topic)
}

func (c *Communicator) PublishToTopic(ctx context.Context, topic string, data []byte, opts ...pubsub.PubOpt) error {
	return c.p2pSrv.PublishToTopic(ctx, topic, data, opts...)
}

func (c *Communicator) SubscribeToTopic(topic string, opts ...pubsub.SubOpt) (*pubsub.Subscription, error) {
	return c.p2pSrv.SubscribeToTopic(topic, opts...)
}
