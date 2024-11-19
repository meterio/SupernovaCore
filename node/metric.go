package node

import (
		cfg "github.com/cometbft/cometbft/config"
		cmtnode "github.com/cometbft/cometbft/node"
)

func DefaultMetricsProvider(config *cfg.InstrumentationConfig) cmtnode.MetricsProvider {
	return cmtnode.DefaultMetricsProvider(config)
}
