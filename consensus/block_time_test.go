package consensus

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/libs/kv"
)

var (
	TestAddr = common.HexToAddress("0x7567d83b7b8d80addcb281a71d54fc7b3364ffed")
)

func buildGenesis(kv kv.GetPutter, proc func() error) *block.Block {
	blk, err := new(genesis.Builder).
		Timestamp(uint64(time.Now().Unix())).
		Build()
	if err != nil {
		fmt.Println("ERROR: ", err)
	}
	return blk
}
