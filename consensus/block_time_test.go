package consensus

import (
	"fmt"
	"time"

	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/genesis"
	"github.com/meterio/meter-pov/kv"
	"github.com/meterio/meter-pov/meter"
)

var (
	TestAddr, _ = meter.ParseAddress("0x7567d83b7b8d80addcb281a71d54fc7b3364ffed")
)

func initLogger() {
}

func buildGenesis(kv kv.GetPutter, proc func() error) *block.Block {
	blk, err := new(genesis.Builder).
		Timestamp(uint64(time.Now().Unix())).
		Build()
	if err != nil {
		fmt.Println("ERROR: ", err)
	}
	return blk
}
