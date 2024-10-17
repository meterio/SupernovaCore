package consensus

import (
	"context"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtproxy "github.com/cometbft/cometbft/proxy"
)

type Executor struct {
	conn cmtproxy.AppConnConsensus
}

func NewExecutor(proxyApp cmtproxy.AppConns) *Executor {
	return &Executor{conn: proxyApp.Consensus()}
}

func (e *Executor) InitChain(req *abcitypes.InitChainRequest) (*abcitypes.InitChainResponse, error) {
	return e.conn.InitChain(context.TODO(), req)
}

func (e *Executor) PrepareProposal(req *abcitypes.PrepareProposalRequest) (*abcitypes.PrepareProposalResponse, error) {
	return e.conn.PrepareProposal(context.TODO(), req)
}

func (e *Executor) ProcessProposal(req *abcitypes.ProcessProposalRequest) (*abcitypes.ProcessProposalResponse, error) {
	return e.conn.ProcessProposal(context.TODO(), req)
}

func (e *Executor) ExtendVote(req *abcitypes.ExtendVoteRequest) (*abcitypes.ExtendVoteResponse, error) {
	return e.conn.ExtendVote(context.TODO(), req)
}

func (e *Executor) VerifyVoteExtension(req *abcitypes.VerifyVoteExtensionRequest) (*abcitypes.VerifyVoteExtensionResponse, error) {
	return e.conn.VerifyVoteExtension(context.TODO(), req)
}

func (e *Executor) FinalizeBlock(req *abcitypes.FinalizeBlockRequest) (*abcitypes.FinalizeBlockResponse, error) {
	return e.conn.FinalizeBlock(context.TODO(), req)
}

func (e *Executor) Commit() (*abcitypes.CommitResponse, error) {
	return e.conn.Commit(context.TODO())
}
