package commands

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	cfg "github.com/cometbft/cometbft/config"
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

// GenFilePV generates a new validator with randomly generated private key
// and sets the filePaths, but does not call Save().
func GenFilePV(keyFilePath, stateFilePath string) *privval.FilePV {
	blsPrivKey := types.NewBlsPrivKey()
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
		if _, err := types.LoadOrGenNodeKey(nodeKeyFile); err != nil {
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
