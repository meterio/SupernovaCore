package block

import "fmt"

// definition for DraftQC
type DraftQC struct {
	//Height/QCround must be the same with QCNode.Height/QCnode.Round
	QCNode *DraftBlock // this is the QCed block
	QC     *QuorumCert // this is the actual QC that goes into the next block
}

func NewDraftQC(qc *QuorumCert, qcNode *DraftBlock) *DraftQC {
	return &DraftQC{
		QCNode: qcNode,
		QC:     qc,
	}
}

func (qc *DraftQC) ToString() string {
	if qc.QCNode != nil {
		if qc.QCNode.ProposedBlock.ID() == qc.QC.BlockID && qc.QCNode.Round == qc.QC.Round {
			return fmt.Sprintf("DraftQC(E%v.R%v -> %v)", qc.QC.Epoch, qc.QC.Round, qc.QCNode.ProposedBlock.ID().ToBlockShortID())
		} else {
			return fmt.Sprintf("DraftQC(E%v.R%v, qcNode:(#%v,R:%v))", qc.QC.Epoch, qc.QC.Round, qc.QCNode.Height, qc.QCNode.Round)
		}
	} else {
		return fmt.Sprintf("DraftQC(E%v.R%v, qcNode:nil)", qc.QC.Epoch, qc.QC.Round)
	}
}
