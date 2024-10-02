package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"

	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/libs/lvldb"
	"github.com/meterio/supernova/types"
	"gopkg.in/urfave/cli.v1"
)

func openMainDB(ctx *cli.Context) (*lvldb.LevelDB, *genesis.Genesis) {
	baseDir := ctx.String(dataDirFlag.Name)
	gene := genesis.LoadGenesis(baseDir)
	// init block chain config
	dbFilePath := ctx.String(dataDirFlag.Name)
	instanceDir := filepath.Join(dbFilePath, fmt.Sprintf("instance-%d", gene.ChainId))
	if _, err := fdlimit.Raise(5120 * 4); err != nil {
		panic(fmt.Sprintf("failed to increase fd limit due to %v", err))
	}
	limit, err := fdlimit.Current()
	if err != nil {
		panic(fmt.Sprintf("failed to get fd limit due to: %v", err))
	}
	if limit <= 1024 {
		fmt.Printf("low fd limit, increase it if possible limit = %v\n", limit)
	} else {
		fmt.Println("fd limit", "limit", limit)
	}
	fileCache := 1024

	dir := filepath.Join(instanceDir, "main.db")
	db, err := lvldb.New(dir, lvldb.Options{
		CacheSize:              128,
		OpenFilesCacheCapacity: fileCache,
	})
	if err != nil {
		panic(fmt.Sprintf("open chain database [%v]: %v", dir, err))
	}
	return db, gene
}

func fullVersion() string {
	versionMeta := "release"
	if gitTag == "" {
		versionMeta = "dev"
	}
	return fmt.Sprintf("%s-%s-%s", version, gitCommit, versionMeta)
}

func initChain(gene *genesis.Genesis, mainDB *lvldb.LevelDB) *chain.Chain {
	genesisBlock, err := gene.Build()
	if err != nil {
		fatal("build genesis block: ", err)
	}

	chain, err := chain.New(mainDB, genesisBlock, gene.ValidatorSet(), true)
	if err != nil {
		fatal("initialize block chain:", err)
	}
	return chain
}

var (
	InvalidRevision = errors.New("invalid revision")
)

func loadBlockByRevision(meterChain *chain.Chain, revision string) (*block.Block, error) {
	if revision == "" || revision == "best" {
		return meterChain.BestBlock(), nil
	}
	if len(revision) == 66 || len(revision) == 64 {
		blockID, err := types.ParseBytes32(revision)
		if err != nil {
			return nil, InvalidRevision
		}
		return meterChain.GetBlock(blockID)
	}
	n, err := strconv.ParseUint(revision, 0, 0)
	if err != nil {
		return nil, InvalidRevision
	}
	if n > math.MaxUint32 {
		return nil, InvalidRevision
	}
	return meterChain.GetTrunkBlock(uint32(n))
}

func numberAsKey(num uint32) []byte {
	var key [4]byte
	binary.BigEndian.PutUint32(key[:], num)
	return key[:]
}
