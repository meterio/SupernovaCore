package main

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"path"
	"strings"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/kv"
	"github.com/meterio/supernova/lvldb"
	"gopkg.in/urfave/cli.v1"
)

func loadStashAction(ctx *cli.Context) error {
	dataDir := ctx.String(dataDirFlag.Name)
	stashDir := path.Join(dataDir, "tx.stash")
	db, err := lvldb.New(stashDir, lvldb.Options{})
	if err != nil {
		slog.Error("create tx stash", "err", err)
		return nil
	}
	defer db.Close()

	iter := db.NewIterator(*kv.NewRangeWithBytesPrefix(nil))
	for iter.Next() {
		var tx cmttypes.Tx
		if err := rlp.DecodeBytes(iter.Value(), &tx); err != nil {
			slog.Warn("decode stashed tx", "err", err)
			if err := db.Delete(iter.Key()); err != nil {
				slog.Warn("delete corrupted stashed tx", "err", err)
			}
		} else {
			slog.Info("found tx: ", "id", tx.Hash(), "raw", hex.EncodeToString(tx))
		}
	}
	return nil
}

func genKeyAction(ctx *cli.Context) error {
	initLogger()

	name := ctx.String(rawFlag.Name)
	key := []byte(name)
	slog.Info(fmt.Sprintf("Key for %v", name), "is", hex.EncodeToString(key))
	return nil
}

func loadRawAction(ctx *cli.Context) error {
	initLogger()

	mainDB, _ := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	key := ctx.String(keyFlag.Name)
	parsedKey, err := hex.DecodeString(strings.Replace(key, "0x", "", 1))
	if err != nil {
		slog.Error("could not decode hex key", "err", err)
		return nil
	}
	raw, err := mainDB.Get(parsedKey)
	if err != nil {
		slog.Error("could not find key in database", "err", err, "key", key)
		return nil
	}
	slog.Info("Loaded key from db", "key", hex.EncodeToString(parsedKey), "val", hex.EncodeToString(raw))
	return nil
}

func loadBlockAction(ctx *cli.Context) error {
	mainDB, gene := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	meterChain := initChain(ctx, gene, mainDB)

	blk, err := loadBlockByRevision(meterChain, ctx.String(revisionFlag.Name))
	if err != nil {
		panic("could not load block")
	}
	rawBlk, err := meterChain.GetBlockRaw(blk.ID())
	if err != nil {
		panic("could not load block raw")
	}
	slog.Info("Loaded Block", "revision", ctx.String(revisionFlag.Name))
	fmt.Println(blk.String())
	rawQC := hex.EncodeToString(blk.QC.ToBytes())
	slog.Info("Raw QC", "hex", rawQC)
	slog.Info("Raw", "hex", hex.EncodeToString(rawBlk))
	return nil
}

func loadHashAction(ctx *cli.Context) error {
	mainDB, _ := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	num := uint32(ctx.Int(heightFlag.Name))
	numKey := numberAsKey(num)
	val, err := mainDB.Get(append(hashKeyPrefix, numKey...))
	if err != nil {
		fmt.Println("could not get index trie root", err)
	}
	fmt.Printf("block hash for %v is %v \n", num, hex.EncodeToString(val))
	return nil
}

func peekAction(ctx *cli.Context) error {
	mainDB, gene := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	meterChain := initChain(ctx, gene, mainDB)

	var (
		network = ctx.String(networkFlag.Name)
		datadir = ctx.String(dataDirFlag.Name)
		err     error
	)

	// Read/Decode/Display Block
	fmt.Println("------------ Pointers ------------")
	fmt.Println("Datadir: ", datadir)
	fmt.Println("Network: ", network)
	fmt.Println("-------------------------------")

	// Read Best QC
	val, err := readBestQC(mainDB)
	if err != nil {
		panic(err)
	}
	fmt.Println("Best QC: ", val)

	// Read Best
	val, err = readBest(mainDB)
	if err != nil {
		panic(fmt.Sprintf("could not read best: %v", err))
	}
	fmt.Println("Best Block:", val)

	bestBlk, err := loadBlockByRevision(meterChain, "best")
	if err != nil {
		panic("could not read best block")
	}
	fmt.Println("Best Block (Decoded): \n", bestBlk.String())

	return nil
}
