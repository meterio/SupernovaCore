package consensus

import (
	"fmt"
	"time"

	cmtdb "github.com/cometbft/cometbft-db"
	"github.com/ethereum/go-ethereum/common"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/genesis"
)

var (
	TestAddr = common.HexToAddress("0x7567d83b7b8d80addcb281a71d54fc7b3364ffed")
)

func buildGenesis(db cmtdb.DB, proc func() error) *block.Block {
	blk, err := new(genesis.Builder).
		Timestamp(uint64(time.Now().Unix())).
		Build()
	if err != nil {
		fmt.Println("ERROR: ", err)
	}
	return blk
}
