package p2p

import (
	"context"
	"fmt"

	"github.com/kr/pretty"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	snmsg "github.com/meterio/supernova/libs/message"
	"github.com/prysmaticlabs/prysm/v5/monitoring/tracing"
	"github.com/prysmaticlabs/prysm/v5/monitoring/tracing/trace"
	"github.com/sirupsen/logrus"
)

// Send a message to a specific peer. The returned stream may be used for reading, but has been
// closed for writing.
//
// When done, the caller must Close or Reset on the stream.
func (s *Service) Send(ctx context.Context, message interface{}, baseTopic string, pid peer.ID) (network.Stream, error) {
	ctx, span := trace.StartSpan(ctx, "p2p.Send")
	defer span.End()
	if err := VerifyTopicMapping(baseTopic, message); err != nil {
		return nil, err
	}
	topic := baseTopic + s.Encoding().ProtocolSuffix()
	span.SetAttributes(trace.StringAttribute("topic", topic))

	log.WithFields(logrus.Fields{
		"topic":   topic,
		"request": pretty.Sprint(message),
	}).Tracef("Sending RPC request to peer %s", pid.String())

	// Apply max dial timeout when opening a new stream.
	ctx, cancel := context.WithTimeout(ctx, maxDialTimeout)
	defer cancel()

	s.logger.Debug(fmt.Sprintf("rpc call %v", message.(*snmsg.RPCEnvelope).MsgType), "toPeer", pid)
	stream, err := s.host.NewStream(ctx, pid, protocol.ID(topic))
	if err != nil {
		tracing.AnnotateError(span, err)
		return nil, err
	}
	// // do not encode anything if we are sending a metadata request
	// if baseTopic != RPCMetaDataTopicV1 && baseTopic != RPCMetaDataTopicV2 {
	// 	castedMsg, ok := message.(ssz.Marshaler)
	// 	if !ok {
	// 		return nil, errors.Errorf("%T does not support the ssz marshaller interface", message)
	// 	}
	// 	if _, err := s.Encoding().EncodeWithMaxLength(stream, castedMsg); err != nil {
	// 		tracing.AnnotateError(span, err)
	// 		_err := stream.Reset()
	// 		_ = _err
	// 		return nil, err
	// 	}
	// }
	bs, err := message.(*snmsg.RPCEnvelope).MarshalSSZ()
	if err != nil {
		fmt.Println("marshal error:", err)
	}

	_, err = stream.Write(bs)

	// Close stream for writing.
	if err := stream.CloseWrite(); err != nil {
		tracing.AnnotateError(span, err)
		_err := stream.Reset()
		_ = _err
		return nil, err
	}

	return stream, nil
}
