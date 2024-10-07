package types

import (
	"fmt"
	"os"

	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto/tmhash"
)

// ChecksummedGenesisDoc combines a GenesisDoc together with its
// SHA256 checksum.
type ChecksummedGenesisDoc struct {
	GenesisDoc     *GenesisDoc
	Sha256Checksum []byte
}

// GenesisDocProvider returns a GenesisDoc together with its SHA256 checksum.
// It allows the GenesisDoc to be pulled from sources other than the
// filesystem, for instance from a distributed key-value store cluster.
type GenesisDocProvider func() (ChecksummedGenesisDoc, error)

// DefaultGenesisDocProviderFunc returns a GenesisDocProvider that loads
// the GenesisDoc from the config.GenesisFile() on the filesystem.
func DefaultGenesisDocProviderFunc(config *cfg.Config) GenesisDocProvider {
	return func() (ChecksummedGenesisDoc, error) {
		// FIXME: find a way to stream the file incrementally,
		// for the JSON	parser and the checksum computation.
		// https://github.com/cometbft/cometbft/issues/1302
		jsonBlob, err := os.ReadFile(config.GenesisFile())
		if err != nil {
			return ChecksummedGenesisDoc{}, fmt.Errorf("couldn't read GenesisDoc file: %w", err)
		}
		incomingChecksum := tmhash.Sum(jsonBlob)
		genDoc, err := GenesisDocFromJSON(jsonBlob)
		if err != nil {
			return ChecksummedGenesisDoc{}, err
		}
		return ChecksummedGenesisDoc{GenesisDoc: genDoc, Sha256Checksum: incomingChecksum}, nil
	}
}
