package types_test

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestRLPAppend(t *testing.T) {
	list := []byte{0x1, 0x2, 0x3}
	b, _ := rlp.EncodeToBytes(list)
	fmt.Printf("%x\n", b)

	b = append(b, 0x4)
	list = append(list, 0x4)
	b2, _ := rlp.EncodeToBytes(list)
	fmt.Printf("%x\n", b2)
	b[0] = 0x84
	fmt.Printf("%x\n", b)
	r := make([]byte, 0)
	rlp.DecodeBytes(b, &r)
	fmt.Printf("%x\n", r)
}

func decodeLen(raw []byte) (prefixLen, length uint64) {
	b0 := raw[0]
	l := uint8(b0) - 0xf7
	fmt.Println("l=", l)
	bs := make([]byte, 0)
	for i := 0; i < int(l); i++ {
		bs = append(bs, raw[i+1])
	}
	for j := 0; j <= int(7-l); j++ {
		bs = append([]byte{0x00}, bs...)
	}
	fmt.Printf("%x\n", bs)
	return uint64(l + 1), binary.BigEndian.Uint64(bs)
}

func packLen(num uint64) []byte {
	bs := make([]byte, 8)
	binary.BigEndian.PutUint64(bs, num)
	index := 0
	for i := 0; i < 8; i++ {
		if bs[i] != 0x00 {
			index = i
			break
		}
	}
	l := 8 - index
	prefix := []byte{0xf7 + uint8(l)}
	prefix = append(prefix, bs[index:]...)
	return prefix
}
