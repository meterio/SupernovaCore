package consensus

import (
	"github.com/meterio/supernova/block"
)

func (r *Reactor) Relay(msg block.ConsensusMessage, rawMsg []byte) {
	// only relay proposal message
	if proposalMsg, ok := msg.(*block.PMProposalMessage); ok {
		round := proposalMsg.Round
		peers := r.Pacemaker.epochState.GetRelayPeers(round)
		if len(peers) > 0 {
			for _, peer := range peers {
				outQueue.Add(*peer, msg, rawMsg, true)
			}
		}
	}

}

func (r *Reactor) Send(msg block.ConsensusMessage, peers ...*ConsensusPeer) {
	rawMsg, err := r.Pacemaker.MarshalMsg(msg)
	if err != nil {
		r.logger.Warn("could not marshal msg", "err", err)
		return
	}

	if len(peers) > 0 {
		for _, peer := range peers {
			outQueue.Add(*peer, msg, rawMsg, false)
		}
	}
}
