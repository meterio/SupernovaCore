package types

import "encoding/binary"

type Extension struct {
	Epoch     uint64
	BlockID   []byte
	Extension []byte
}

func GetMsgHashForVoteExtension(epoch uint64, blockID, extension []byte) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, epoch)
	return append(append(b, blockID...), extension...)
}
