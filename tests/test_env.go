package tests

import (
	"github.com/meterio/meter-pov/state"
)

type TestEnv struct {
	State       *state.State
	BktCreateTS uint64
	CurrentTS   uint64
	ChainTag    byte
}
