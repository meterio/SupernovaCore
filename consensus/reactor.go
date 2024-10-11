// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	sha256 "crypto/sha256"
	"encoding/base64"
	b64 "encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtproxy "github.com/cometbft/cometbft/proxy"
	crypto "github.com/ethereum/go-ethereum/crypto"
	lru "github.com/hashicorp/golang-lru"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/comm"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
)

var (
	validQCs, _ = lru.New(256)
)

type ReactorConfig struct {
	EpochMBlockCount uint32
	MinCommitteeSize int
	MaxCommitteeSize int
}

// -----------------------------------------------------------------------------
// Reactor defines a reactor for the consensus service.
type Reactor struct {
	txpool   *txpool.TxPool
	comm     *comm.Communicator
	chain    *chain.Chain
	logger   *slog.Logger
	config   *cmtcfg.Config
	SyncDone bool
	proxyApp cmtproxy.AppConns

	// still references above consensuStae, reactor if this node is
	// involved the consensus
	committeeSize uint32
	mapMutex      sync.RWMutex
	knownIPs      map[string]string

	committee     *types.ValidatorSet // current committee on latest block
	lastCommittee *types.ValidatorSet

	committeeIndex   uint32
	inCommittee      bool
	lastKBlockHeight uint32
	curNonce         uint64
	curEpoch         uint64

	blsMaster *types.BlsMaster //this must be allocated as validator
	pacemaker *Pacemaker

	magic [4]byte

	inQueue  *IncomingQueue
	outQueue *OutgoingQueue
}

// NewConsensusReactor returns a new Reactor with config
func NewConsensusReactor(config *cmtcfg.Config, chain *chain.Chain, comm *comm.Communicator, txpool *txpool.TxPool, blsMaster *types.BlsMaster, proxyApp cmtproxy.AppConns) *Reactor {
	prometheus.Register(pmRoundGauge)
	prometheus.Register(curEpochGauge)
	prometheus.Register(lastKBlockHeightGauge)
	prometheus.Register(blocksCommitedCounter)
	prometheus.Register(inCommitteeGauge)
	prometheus.Register(pmRoleGauge)

	r := &Reactor{
		comm:        comm,
		txpool:      txpool,
		chain:       chain,
		logger:      slog.With("pkg", "r"),
		SyncDone:    false,
		magic:       [4]byte{0x01, 0x02, 0x03, 0x04},
		inCommittee: false,
		knownIPs:    make(map[string]string),
		config:      config,
		proxyApp:    proxyApp,

		inQueue:  NewIncomingQueue(),
		outQueue: NewOutgoingQueue(),

		blsMaster: blsMaster,
	}

	// initialize consensus common
	r.logger.Info("my keys", "pubkey", b64.StdEncoding.EncodeToString(blsMaster.PubKey.Marshal()))

	// committee info is stored in the first of Mblock after Kblock
	r.UpdateCurEpoch()

	// initialize pacemaker
	r.pacemaker = NewPacemaker(r)

	return r
}

// OnStart implements BaseService by subscribing to events, which later will be
// broadcasted to other peers and starting state if we're not in fast sync.
func (r *Reactor) OnStart(ctx context.Context) error {

	go r.outQueue.Start(ctx)

	vset := r.chain.GetBestValidatorSet()
	if vset.Size() <= 1 {
		r.pacemaker.Regulate()
	} else {

		select {
		case <-ctx.Done():
			r.logger.Warn("stop reactor due to context end")
			return nil
		case <-r.comm.Synced():
			r.SyncDone = true
			r.logger.Info("syncing is done")
			r.pacemaker.Regulate()
		}
	}

	return nil
}

func (r *Reactor) GetLastKBlockHeight() uint32 {
	return r.lastKBlockHeight
}

func (r *Reactor) PubKey() bls.PublicKey {
	return r.blsMaster.PubKey
}

func (r *Reactor) PrivKey() bls.SecretKey {
	return r.blsMaster.PrivKey
}

// get the specific round proposer
func (r *Reactor) getRoundProposer(round uint32) *types.Validator {
	size := len(r.committee.Validators)
	if size == 0 {
		return &types.Validator{}
	}
	return r.committee.GetByIndex(round)
}

func (r *Reactor) amIRoundProproser(round uint32) bool {
	p := r.getRoundProposer(round)
	return bytes.Equal(p.PubKey.Marshal(), r.blsMaster.PubKey.Marshal())
}

// it is used for temp calculate committee set by a given nonce in the fly.
// also return the committee
func (r *Reactor) calcCommitteeByNonce(name string, vset *types.ValidatorSet, nonce uint64) (*types.ValidatorSet, uint32, bool) {
	fmt.Println("calc committee by nonce", name)

	newVset := vset.SortWithNonce(nonce)

	r.logger.Info(fmt.Sprintf("cal %s committee", name), "nonce", nonce)

	committee := newVset
	// the full list is stored in currCommittee, sorted.
	// To become a validator (real member in committee), must repond the leader's
	// announce. Validators are stored in r.conS.Vlidators
	if len(committee.Validators) > 0 {
		for i, val := range committee.Validators {
			if bytes.Equal(val.PubKey.Marshal(), r.PubKey().Marshal()) {

				return committee, uint32(i), true
			}
		}
	} else {
		r.logger.Error("committee is empty, potential error config with delegates.json")
	}

	return committee, 0, false
}

func (r *Reactor) GetMyNetAddr() *types.NetAddress {
	if r.committee != nil && len(r.committee.Validators) > 0 {
		v := r.committee.GetByIndex(r.committeeIndex)
		return types.NewNetAddressFromNetIP(v.IP, v.Port)
	}
	return &types.NetAddress{IP: net.IP{}, Port: 0}
}

func (r *Reactor) GetMyName() string {
	if r.committee != nil && len(r.committee.Validators) > 0 {
		return r.committee.GetByIndex(r.committeeIndex).Name
	}
	return "unknown"
}

func (r *Reactor) IsMe(peer *ConsensusPeer) bool {
	if r.committee != nil && len(r.committee.Validators) > 0 {
		return r.committee.GetByIndex(r.committeeIndex).IP.String() == peer.IP
	}
	return false
}

func (r *Reactor) SchedulePacemakerRegulate() {
	r.pacemaker.scheduleRegulate()
}

func (r *Reactor) PrepareEnvForPacemaker() error {

	bestKBlock, err := r.chain.BestKBlock()
	if err != nil {
		fmt.Println("could not get best KBlock", err)
		return errors.New("could not get best KBlock")
	}
	bestBlock := r.chain.BestBlock()
	bestIsKBlock := bestBlock.IsKBlock() || bestBlock.Header().Number() == 0

	_, err = r.UpdateCurEpoch()
	if err != nil {
		return err
	}
	r.logger.Info("prepare env for pacemaker", "nonce", r.curNonce, "bestK", bestKBlock.Number(), "bestIsKBlock", bestIsKBlock, "epoch", r.curEpoch)

	return nil
}

func (r *Reactor) combinePubKey(ecdsaPub *ecdsa.PublicKey, blsPub *bls.PublicKey) string {
	ecdsaPubBytes := crypto.FromECDSAPub(ecdsaPub)
	ecdsaPubB64 := b64.StdEncoding.EncodeToString(ecdsaPubBytes)

	blsPubBytes := (*blsPub).Marshal()
	blsPubB64 := b64.StdEncoding.EncodeToString(blsPubBytes)

	return strings.Join([]string{ecdsaPubB64, blsPubB64}, ":::")
}

func (r *Reactor) getNameByIP(ip net.IP) string {
	defer r.mapMutex.RUnlock()
	r.mapMutex.RLock()
	if name, exist := r.knownIPs[ip.String()]; exist {
		return name
	}
	return ""
}

func (r *Reactor) UpdateCurEpoch() (bool, error) {
	best := r.chain.BestBlock()
	bestK, err := r.chain.BestKBlock()
	if err != nil {
		r.logger.Error("could not get bestK", "err", err)
		return false, errors.New("could not get best KBlock")
	}

	var nonce uint64
	epoch := uint64(0)

	nonce = bestK.Nonce()
	epoch = bestK.GetBlockEpoch() + 1
	if epoch >= r.curEpoch && r.curNonce != nonce {
		r.logger.Info(fmt.Sprintf("Entering epoch %v", epoch), "lastEpoch", r.curEpoch)
		r.logger.Info("---------------------------------------------------------")

		vset := r.chain.GetValidatorSet(best.Number())

		fmt.Println("validator set: ", vset)
		r.logger.Info("validator set", "len", vset.Size())
		if r.committee != nil && r.committee.Size() > 0 {
			r.logger.Info("committee", "size", r.committee.Size())
			r.lastCommittee = r.committee
		} else {
			lastBestKHeight := bestK.LastKBlockHeight()
			var lastNonce uint64
			var lastBestK *block.Block

			lastBestK, err = r.chain.GetTrunkBlock(lastBestKHeight)
			if err != nil {
				r.logger.Error("could not get trunk block", "err", err)
			} else {
				lastNonce = lastBestK.Nonce()
			}

			r.logger.Info("lastBestK is ", "blk", lastBestK.CompactString())
			lastVSet := r.chain.GetValidatorSet(lastBestK.Number())

			r.lastCommittee, _, _ = r.calcCommitteeByNonce("last", lastVSet, lastNonce)

		}
		r.committee, r.committeeIndex, r.inCommittee = r.calcCommitteeByNonce("current", vset, nonce)
		r.committeeSize = uint32(r.committee.Size())
		r.PrintCommittee()
		// r.PrintCommittee()

		// update nonce
		r.curNonce = nonce

		if r.inCommittee {
			myAddr := r.committee.GetByIndex(r.committeeIndex).IP
			myName := r.committee.GetByIndex(r.committeeIndex).Name

			r.logger.Info("I'm IN committee !!!", "myName", myName, "myIP", myAddr.String())
			inCommitteeGauge.Set(1)
		} else {
			r.logger.Info("I'm NOT in committee")
			inCommitteeGauge.Set(0)
		}
		r.lastKBlockHeight = bestK.Number()
		lastKBlockHeightGauge.Set(float64(bestK.Number()))

		lastEpoch := r.curEpoch
		r.curEpoch = epoch
		curEpochGauge.Set(float64(r.curEpoch))
		r.logger.Info("---------------------------------------------------------")
		r.logger.Info(fmt.Sprintf("Entered epoch %d", r.curEpoch), "lastEpoch", lastEpoch)
		r.logger.Info("---------------------------------------------------------")
		return true, nil
	}
	return false, nil
}

// ------------------------------------
// UTILITY
// ------------------------------------
func (r *Reactor) OnReceiveMsg(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	r.logger.Debug("before receive", "alloc", types.PrettyStorage(m.Alloc), "sys", types.PrettyStorage(m.Sys))

	data, err := io.ReadAll(req.Body)
	if err != nil {
		r.logger.Error("Unrecognized payload", "err", err)
		return
	}
	mi, err := r.UnmarshalMsg(data)
	if err != nil {
		r.logger.Error("Unmarshal error", "err", err, "from", req.RemoteAddr)
		return
	}
	defer func() {
		var ma runtime.MemStats
		runtime.ReadMemStats(&ma)
		r.logger.Debug(fmt.Sprintf("after receive %s", mi.Msg.GetType()), "allocDiff", types.PrettyStorage(ma.Alloc-m.Alloc), "sysDiff(KB)", types.PrettyStorage(ma.Sys-m.Sys))
	}()

	r.AddIncoming(*mi, data)

}

func (r *Reactor) AddIncoming(mi IncomingMsg, data []byte) {
	msg, peer := mi.Msg, mi.Peer
	typeName := mi.Msg.GetType()

	if msg.GetEpoch() < r.curEpoch {
		r.logger.Info(fmt.Sprintf("outdated %s, dropped ...", msg.String()), "peer", peer.String())
		return
	}

	if msg.GetEpoch() == r.curEpoch {
		signerIndex := msg.GetSignerIndex()
		if int(signerIndex) >= r.committee.Size() {
			r.logger.Warn("index out of range for signer, dropped ...", "peer", peer, "msg", msg.GetType())
			return
		}
		signer := r.committee.GetByIndex(signerIndex)

		if !msg.VerifyMsgSignature(signer.PubKey) {
			r.logger.Error("invalid signature, dropped ...", "peer", peer, "msg", msg.String(), "signer", signer.Name)
			return
		}
		mi.Signer.IP = signer.IP.String()
		mi.Signer.Name = signer.Name
	}

	// sanity check for messages
	switch m := msg.(type) {
	case *block.PMProposalMessage:
		blk := m.DecodeBlock()
		if blk == nil {
			r.logger.Error("Invalid PMProposal: could not decode proposed block")
			return
		}

	case *block.PMTimeoutMessage:
		qcHigh := m.DecodeQCHigh()
		if qcHigh == nil {
			r.logger.Error("Invalid QCHigh: could not decode qcHigh")
		}
	}

	fromMyself := peer.IP == r.GetMyNetAddr().IP.String()

	if msg.GetEpoch() == r.curEpoch {
		err := r.inQueue.Add(mi)
		if err != nil {
			return
		}

		// relay the message if these two conditions are met:
		// 1. the original message is not sent by myself
		// 2. it's a proposal message
		if !fromMyself && typeName == "PMProposal" {
			r.Relay(mi.Msg, data)
		}
	} else {
		time.AfterFunc(time.Second, func() {
			r.logger.Info(fmt.Sprintf("future message %s in epoch %d, process after 1s ...", msg.GetType(), msg.GetEpoch()), "curEpoch", r.curEpoch)
			r.AddIncoming(mi, data)
		})
	}
}

func (r *Reactor) ValidateQC(b *block.Block, escortQC *block.QuorumCert) bool {
	var valid bool
	var err error

	h := sha256.New()
	h.Write(escortQC.ToBytes())
	qcID := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if validQCs.Contains(qcID) {
		return true
	}
	// KBlock should be verified with last committee
	r.logger.Debug("validate qc", "iskblock", b.IsKBlock(), "number", b.Number(), "best", r.chain.BestBlock().Number())
	if b.IsKBlock() && b.Number() > 0 && b.Number() <= r.chain.BestBlock().Number() {
		r.logger.Info("verifying with last committee")
		start := time.Now()
		valid, err = b.VerifyQC(escortQC, r.blsMaster, r.lastCommittee)
		if valid && err == nil {
			r.logger.Debug("validated QC with last committee", "elapsed", types.PrettyDuration(time.Since(start)))
			validQCs.Add(qcID, true)
			return true
		}
		r.logger.Warn(fmt.Sprintf("validate %s with last committee FAILED", escortQC.CompactString()), "size", r.lastCommittee.Size(), "err", err)

	}

	// validate with current committee
	start := time.Now()
	if r.committee.Size() <= 0 {
		fmt.Println("verify QC with empty r.committee")
		return false
	}
	valid, err = b.VerifyQC(escortQC, r.blsMaster, r.committee)
	if valid && err == nil {
		r.logger.Info(fmt.Sprintf("validated %s", escortQC.CompactString()), "elapsed", types.PrettyDuration(time.Since(start)))
		validQCs.Add(qcID, true)
		return true
	}
	r.logger.Warn(fmt.Sprintf("validate %s FAILED", escortQC.CompactString()), "err", err, "committeeSize", r.committee.Size())
	return false
}

func (r *Reactor) peakCommittee(committee *types.ValidatorSet) string {
	s := make([]string, 0)
	if committee.Size() > 6 {
		for index, val := range committee.Validators[:3] {
			s = append(s, fmt.Sprintf("#%-4v %v", index, val.String()))
		}
		s = append(s, "...")
		for index, val := range committee.Validators[committee.Size()-3:] {
			s = append(s, fmt.Sprintf("#%-4v %v", index+committee.Size()-3, val.String()))
		}
	} else {
		for index, val := range committee.Validators {
			s = append(s, fmt.Sprintf("#%-2v %v", index, val.String()))
		}
	}
	return strings.Join(s, "\n")
}

func (r *Reactor) PrintCommittee() {
	fmt.Printf("* Current Committee (%d):\n%s\n\n", r.committee.Size(), r.peakCommittee(r.committee))

	fmt.Printf("Last Committee (%d):\n%s\n\n", r.lastCommittee.Size(), r.peakCommittee(r.lastCommittee))
}

func (r *Reactor) IncomingQueueLen() int {
	return r.inQueue.Len()
}

func (r *Reactor) OutgoingQueueLen() int {
	return r.outQueue.Len()
}

func (r *Reactor) SignMessage(msg block.ConsensusMessage) {
	msgHash := msg.GetMsgHash()
	sig := r.PrivKey().Sign(msgHash[:])
	msg.SetMsgSignature(sig.Marshal())
}
