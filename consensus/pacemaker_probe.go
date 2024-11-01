package consensus

// This is part of pacemaker that in charge of:
// 1. provide probe for debug

import (
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

type BlockProbe struct {
	Height uint32
	Round  uint32
	ID     types.Bytes32
}
type PMProbeResult struct {
	CurRound       uint32
	InCommittee    bool
	CommitteeIndex int
	CommitteeSize  int

	LastVotingHeight uint32
	LastOnBeatRound  uint32
	QCHigh           *block.QuorumCert
	LastCommitted    *BlockProbe

	ProposalCount int
}

func (p *Pacemaker) Probe() *PMProbeResult {
	result := &PMProbeResult{
		CurRound:       p.currentRound,
		InCommittee:    p.epochState.inCommittee,
		CommitteeIndex: int(p.epochState.CommitteeIndex()),
		CommitteeSize:  int(p.epochState.CommitteeSize()),

		LastVotingHeight: p.lastVotingHeight,
		LastOnBeatRound:  uint32(p.lastOnBeatRound),
	}
	if p.QCHigh != nil && p.QCHigh.QC != nil {
		result.QCHigh = p.QCHigh.QC
	}
	if p.lastCommitted != nil {
		rlp.EncodeToBytes(p.lastCommitted)
		result.LastCommitted = &BlockProbe{Height: p.lastCommitted.Number(), ID: p.lastCommitted.ID()}
	}
	result.ProposalCount = p.chain.DraftLen()

	return result

}
