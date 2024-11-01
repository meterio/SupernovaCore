package consensus

import (
	sha256 "crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/meterio/supernova/block"
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
	// validate with current committee
	if p.epochState.CommitteeSize() <= 0 {
		fmt.Println("verify QC with empty p.committee")
		return false
	}
	valid, err = b.VerifyQC(escortQC, p.blsMaster, p.epochState.committee)
	if valid && err == nil {
		p.logger.Debug(fmt.Sprintf("validated %s", escortQC.CompactString()))
		validQCs.Add(qcID, true)
		return true
	}
	p.logger.Warn(fmt.Sprintf("validate %s FAILED", escortQC.CompactString()), "err", err, "committeeSize", p.epochState.CommitteeSize())
	return false
}
