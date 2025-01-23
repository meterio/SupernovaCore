package block

import (
	"fmt"
)

// definition for DraftBlock
type DraftBlock struct {
	Msg           ConsensusMessage
	Height        uint32
	Round         uint32
	Parent        *DraftBlock
	Justify       *DraftQC
	Committed     bool // used for DraftBlock created from database
	ProposedBlock *Block

	// local derived data structure, re-exec all txs and get
	// states. If states are match proposer, then vote, otherwise decline.

	SuccessProcessed bool
	ProcessError     error
}

func (pb *DraftBlock) ToString() string {
	if pb == nil {
		return "DraftBlock(nil)"
	}
	if pb.Committed {
		return fmt.Sprintf("Block{(H:%v,R:%v), QC:(E%v.R%v), Parent:%v}",
			pb.Height, pb.Round, pb.ProposedBlock.QC.Epoch, pb.ProposedBlock.QC.Round, pb.ProposedBlock.ParentID().ToBlockShortID())
	}
	if pb.Parent != nil {
		return fmt.Sprintf("DraftBlock{(H:%v,R:%v), QC:(E%v.R%v), Parent:(H:%v, H:%v)}",
			pb.Height, pb.Round, pb.Justify.QC.Epoch, pb.Justify.QC.Round, pb.Parent.Height, pb.Parent.Round)
	} else {
		return fmt.Sprintf("DraftBlock{(H:%v,R:%v), QC:(E%v.R%v)}",
			pb.Height, pb.Round, pb.Justify.QC.Epoch, pb.Justify.QC.Round)
	}
}
