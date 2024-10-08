package commands

import (
	"bytes"
	"crypto"
	"fmt"
	"log/slog"

	"github.com/ethereum/go-ethereum/common"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	"github.com/spf13/cobra"

	cfg "github.com/cometbft/cometbft/config"
	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	cmttypes "github.com/cometbft/cometbft/types"
	cmttime "github.com/cometbft/cometbft/types/time"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
)

// InitFilesCmd initializes a fresh CometBFT instance.
var InitFilesCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize CometBFT",
	RunE:  initFiles,
}

func initFiles(*cobra.Command, []string) error {
	return initFilesWithConfig(config)
}

func init() {
	cmtjson.RegisterType((*BLSPubKey)(nil), "bls12-381.pubkey")
	cmtjson.RegisterType((*BLSPrivKey)(nil), "bls12-381.secret")
}

/*
crypto.PubKey

	type PubKey interface {
		Address() Address
		Bytes() []byte
		VerifySignature(msg []byte, sig []byte) bool
		Equals(other PubKey) bool
		Type() string
	}
*/
type BLSPubKey []byte

func (k BLSPubKey) Bytes() []byte {
	return k
}

func (k BLSPubKey) Address() cmttypes.Address {
	return common.BytesToAddress(k).Bytes()
}

func (k BLSPubKey) VerifySignature(msg []byte, sig []byte) bool {
	signature, err := bls.SignatureFromBytes(sig)
	if err != nil {
		return false
	}
	pubkey, err := bls.PublicKeyFromBytes(k)
	if err != nil {
		return false
	}
	return signature.Verify(pubkey, msg)
}

func (k BLSPubKey) Equals(other cmtcrypto.PubKey) bool {
	return bytes.Equal(k, other.Bytes())
}

func (k BLSPubKey) Type() string {
	return "bls12-381.pubkey"
}

var _ crypto.PublicKey = BLSPubKey{}

/*
crypto.PrivKey

	type PrivKey interface {
		Bytes() []byte
		Sign(msg []byte) ([]byte, error)
		PubKey() PubKey
		Equals(other PrivKey) bool
		Type() string
	}
*/

type BLSPrivKey []byte

func NewBlsPrivKey() BLSPrivKey {
	key, _ := bls.RandKey()
	return key.Marshal()
}

func (k BLSPrivKey) Bytes() []byte {
	return k
}

func (k BLSPrivKey) Sign(msg []byte) ([]byte, error) {
	secret, _ := bls.SecretKeyFromBytes(k)
	sig := secret.Sign(msg)
	return sig.Marshal(), nil
}

func (k BLSPrivKey) Equals(other cmtcrypto.PrivKey) bool {
	return bytes.Equal(k, other.Bytes())
}

func (k BLSPrivKey) PubKey() cmtcrypto.PubKey {
	secret, _ := bls.SecretKeyFromBytes(k)
	return BLSPubKey(secret.PublicKey().Marshal())
}

func (k BLSPrivKey) Type() string {
	return "bls12-381.secret"
}

var _ cmtcrypto.PrivKey = BLSPrivKey{}

// GenFilePV generates a new validator with randomly generated private key
// and sets the filePaths, but does not call Save().
func GenFilePV(keyFilePath, stateFilePath string) *privval.FilePV {
	blsPrivKey := NewBlsPrivKey()
	return privval.NewFilePV(blsPrivKey, keyFilePath, stateFilePath)
}

func initFilesWithConfig(config *cfg.Config) error {
	logger := slog.With("cmd", "init")
	// private validator
	fmt.Println("root: ", config.RootDir, config.BaseConfig.RootDir)
	privValKeyFile := config.PrivValidatorKeyFile()
	privValStateFile := config.PrivValidatorStateFile()
	fmt.Println("privValKey", privValKeyFile, "privValState", privValStateFile)
	var pv *privval.FilePV
	if cmn.FileExists(privValKeyFile) {
		pv = privval.LoadFilePV(privValKeyFile, privValStateFile)
		logger.Info("Found private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	} else {
		pv = GenFilePV(privValKeyFile, privValStateFile)
		pv.Save()
		logger.Info("Generated private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	}

	nodeKeyFile := config.NodeKeyFile()
	if cmn.FileExists(nodeKeyFile) {
		logger.Info("Found node key", "path", nodeKeyFile)
	} else {
		if _, err := p2p.LoadOrGenNodeKey(nodeKeyFile); err != nil {
			return err
		}
		logger.Info("Generated node key", "path", nodeKeyFile)
	}

	// genesis file
	genFile := config.GenesisFile()
	if cmn.FileExists(genFile) {
		logger.Info("Found genesis file", "path", genFile)
	} else {
		genDoc := types.GenesisDoc{
			ChainID:         fmt.Sprintf("test-chain-%v", cmn.RandStr(6)),
			GenesisTime:     cmttime.Now(),
			ConsensusParams: cmttypes.DefaultConsensusParams(),
		}
		pubKey, err := pv.GetPubKey()
		if err != nil {
			return fmt.Errorf("can't get pubkey: %w", err)
		}
		genDoc.Validators = []types.GenesisValidator{{
			Address: pubKey.Address(),
			PubKey:  pubKey,
			Power:   10,
			Name:    "local",
			IP:      "127.0.0.1",
			Port:    8670,
		}}

		if err := genDoc.SaveAs(genFile); err != nil {
			return err
		}
		logger.Info("Generated genesis file", "path", genFile)
	}

	return nil
}
