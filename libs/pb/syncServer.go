package pb

import (
	"context"
)

type SyncServer struct{}

func NewSyncServer() *SyncServer {
	return &SyncServer{}
}

func (s *SyncServer) GetStatus(ctx context.Context, req *GetStatusRequest) (*GetStatusResponse, error) {
	// msg := req.Message
	resp := &GetStatusResponse{}
	return resp, nil
}

func (s *SyncServer) NotifyBlock(ctx context.Context, req *NotifyBlockRequest) (*NotifyBlockResponse, error) {
	// msg := req.Message
	resp := &NotifyBlockResponse{}
	return resp, nil
}

func (s *SyncServer) NotifyBlockID(ctx context.Context, req *NotifyBlockIDRequest) (*NotifyBlockIDResponse, error) {
	// msg := req.Message
	resp := &NotifyBlockIDResponse{}
	return resp, nil
}
