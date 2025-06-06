package p2p

import (
	statefeed "github.com/OffchainLabs/prysm/v6/beacon-chain/core/feed/state"
	"github.com/OffchainLabs/prysm/v6/beacon-chain/db"
)

// This is the default queue size used if we have specified an invalid one.
const defaultPubsubQueueSize = 600

// Config for the p2p service. These parameters are set from application level flags
// to initialize the p2p service.
type Config struct {
	NoDiscovery          bool
	EnableUPnP           bool
	StaticPeerID         bool
	StaticPeers          []string
	Discv5BootStrapAddrs []string
	RelayNodeAddr        string
	LocalIP              string
	HostAddress          string
	HostDNS              string
	PrivateKey           string
	DataDir              string
	MetaDataDir          string
	QUICPort             uint
	TCPPort              uint
	UDPPort              uint
	MaxPeers             uint
	QueueSize            uint
	AllowListCIDR        string
	DenyListCIDR         []string
	StateNotifier        statefeed.Notifier
	DB                   db.ReadOnlyDatabase
	// ClockWaiter          startup.ClockWaiter
}

// validateConfig validates whether the values provided are accurate and will set
// the appropriate values for those that are invalid.
func validateConfig(cfg *Config) *Config {
	if cfg.QueueSize == 0 {
		log.Warnf("Invalid pubsub queue size of %d initialized, setting the quese size as %d instead", cfg.QueueSize, defaultPubsubQueueSize)
		cfg.QueueSize = defaultPubsubQueueSize
	}
	return cfg
}
