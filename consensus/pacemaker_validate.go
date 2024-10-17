package consensus

import (
	sha256 "crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

func (p *Pacemaker) ValidateQC(b *block.Block, escortQC *block.QuorumCert) bool {
	var valid bool
	var err error

	h := sha256.New()
	h.Write(escortQC.ToBytes())
	qcID := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if validQCs.Contains(qcID) {
		return true
	}
	//  should be verified with last committee
	p.logger.Debug("validate qc", "number", b.Number(), "best", p.chain.BestBlock().Number())
	if b.IsKBlock() && b.Number() > 0 && b.Number() <= p.chain.BestBlock().Number() {
		p.logger.Info("verifying with last committee")
		start := time.Now()
		valid, err = b.VerifyQC(escortQC, p.blsMaster, p.epochState.lastCommittee)
		if valid && err == nil {
			p.logger.Debug("validated QC with last committee", "elapsed", types.PrettyDuration(time.Since(start)))
			validQCs.Add(qcID, true)
			return true
		}
		p.logger.Warn(fmt.Sprintf("validate %s with last committee FAILED", escortQC.CompactString()), "size", p.epochState.lastCommittee.Size(), "err", err)

	}

	// validate with current committee
	start := time.Now()
	if p.epochState.CommitteeSize() <= 0 {
		fmt.Println("verify QC with empty p.committee")
		return false
	}
	valid, err = b.VerifyQC(escortQC, p.blsMaster, p.epochState.committee)
	if valid && err == nil {
		p.logger.Info(fmt.Sprintf("validated %s", escortQC.CompactString()), "elapsed", types.PrettyDuration(time.Since(start)))
		validQCs.Add(qcID, true)
		return true
	}
	p.logger.Warn(fmt.Sprintf("validate %s FAILED", escortQC.CompactString()), "err", err, "committeeSize", p.epochState.CommitteeSize())
	return false
}
