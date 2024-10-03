package main

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/urfave/cli.v1"
)

func genKeyAction(ctx *cli.Context) error {

	name := ctx.String(rawFlag.Name)
	key := []byte(name)
	slog.Info(fmt.Sprintf("Key for %v", name), "is", hex.EncodeToString(key))
	return nil
}

func loadRawAction(ctx *cli.Context) error {

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

	meterChain := initChain(gene, mainDB)

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

	meterChain := initChain(gene, mainDB)

	var (
		datadir = ctx.String(dataDirFlag.Name)
		err     error
	)

	// Read/Decode/Display Block
	fmt.Println("------------ Pointers ------------")
	fmt.Println("Datadir: ", datadir)
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
