package node

import (
	cfg "github.com/cometbft/cometbft/v2/config"
	cmtnode "github.com/cometbft/cometbft/v2/node"
)

func DefaultMetricsProvider(config *cfg.InstrumentationConfig) cmtnode.MetricsProvider {
	return cmtnode.DefaultMetricsProvider(config)
}
