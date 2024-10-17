package consensus

import (
	"bytes"
	"encoding/json"
	"net"
	"strconv"
	"strings"

	"github.com/meterio/supernova/block"
)

func (p *Pacemaker) UnmarshalMsg(rawData []byte) (*IncomingMsg, error) {
	var params map[string]string
	err := json.NewDecoder(bytes.NewReader(rawData)).Decode(&params)
	if err != nil {
		p.logger.Error("json decode error", "err", err)
		return nil, ErrUnrecognizedPayload
	}
	if strings.Compare(params["version"], p.version) != 0 {
		return nil, ErrVersionMismatch
	}
	peerIP := net.ParseIP(params["ip"])

	// peerPort, err := strconv.ParseUint(params["port"], 10, 16)
	// if err != nil {
	// 	p.logger.Error("unrecognized payload", "err", err)
	// 	return nil, ErrUnrecognizedPayload
	// }
	peerName := p.epochState.GetNameByIP(peerIP.String())
	peer := NewConsensusPeer(peerName, peerIP.String())

	msg, err := block.DecodeMsg(params["raw"])
	if err != nil {
		p.logger.Error("malformatted msg", "msg", msg, "err", err)
		return nil, ErrMalformattedMsg
	}

	msgInfo := newIncomingMsg(msg, *peer, rawData)
	return msgInfo, nil
}

func (p *Pacemaker) MarshalMsg(msg block.ConsensusMessage) ([]byte, error) {
	rawHex, err := block.EncodeMsg(msg)
	if err != nil {
		return make([]byte, 0), err
	}

	myNetAddr := p.getMyNetAddr()
	payload := map[string]interface{}{
		"raw":     rawHex,
		"ip":      myNetAddr.IP.String(),
		"port":    strconv.Itoa(int(myNetAddr.Port)),
		"version": p.version,
	}

	return json.Marshal(payload)
}
