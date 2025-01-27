// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"errors"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/types"
)

// Block block
type Block struct {
	Number     uint32        `json:"number"`
	ID         types.Bytes32 `json:"id"`
	ParentID   types.Bytes32 `json:"parentID"`
	QC         *QC           `json:"qc"`
	Timestamp  uint64        `json:"timestamp"`
	TxCount    int           `json:"txCount"`
	LastKBlock uint32        `json:"lastKBlock"`
	Nonce      uint64        `json:"nonce"`
}

type QC struct {
	Epoch   uint64 `json:"epoch"`
	Round   uint32 `json:"round"`
	BlockID string `json:"blockID"`
}

type BlockProbe struct {
	Height uint32 `json:"height"`
	Round  uint32 `json:"round"`
	// Raw    string `json:"raw"`
}

type PacemakerProbe struct {
	CurRound uint32 `json:"curRound"`

	LastVotingHeight uint32 `json:"lastVotingHeight"`
	LastOnBeatRound  uint32 `json:"lastOnBeatRound"`
	ProposalCount    int    `json:"proposalCount"`

	QCHigh        *QC         `json:"qcHigh"`
	LastCommitted *BlockProbe `json:"lastCommitted"`
}

type ChainProbe struct {
	BestBlock *Block `json:"bestBlock"`
	BestQC    *QC    `json:"bestQC"`
}

func convertQC(qc *block.QuorumCert) (*QC, error) {
	if qc == nil {
		return nil, errors.New("empty qc")
	}
	return &QC{
		Epoch:   qc.Epoch,
		Round:   qc.Round,
		BlockID: qc.BlockID.String(),
	}, nil
}

func convertBlock(b *block.Block) (*Block, error) {
	if b == nil {
		return nil, errors.New("empty block")
	}

	header := b.Header()

	result := &Block{
		Number:     header.Number(),
		ID:         header.ID(),
		ParentID:   header.ParentID,
		Timestamp:  header.Timestamp,
		TxCount:    len(b.Transactions()),
		LastKBlock: header.LastKBlock,
		Nonce:      b.Nonce(),
	}
	var err error
	if b.QC != nil {
		result.QC, err = convertQC(b.QC)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

type ProbeResult struct {
	Name        string `json:"name"`
	PubKey      string `json:"pubkey"`
	PubKeyValid bool   `json:"pubkeyValid"`
	Version     string `json:"version"`

	InCommittee    bool   `json:"inCommittee"`
	CommitteeIndex uint32 `json:"committeeIndex"`
	CommitteeSize  uint32 `json:"committeeSize"`

	BestQC    uint32 `json:"bestQC"`
	BestBlock uint32 `json:"bestBlock"`

	Pacemaker *PacemakerProbe `json:"pacemaker"`
	Chain     *ChainProbe     `json:"chain"`
}

func convertBlockProbe(p *consensus.BlockProbe) (*BlockProbe, error) {
	if p != nil {
		return &BlockProbe{
			Height: p.Height,
			Round:  p.Round,
		}, nil
	}
	return nil, nil
}

func convertPacemakerProbe(r *consensus.PMProbeResult) (*PacemakerProbe, error) {
	if r != nil {
		probe := &PacemakerProbe{
			CurRound: r.CurRound,

			LastVotingHeight: r.LastVotingHeight,
			LastOnBeatRound:  r.LastOnBeatRound,
			ProposalCount:    r.ProposalCount,
		}
		if r.QCHigh != nil {
			probe.QCHigh, _ = convertQC(r.QCHigh)
		}
		probe.LastCommitted, _ = convertBlockProbe(r.LastCommitted)
		return probe, nil
	}
	return nil, nil
}
