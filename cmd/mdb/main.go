package main

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	_ "net/http/pprof"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/txpool"
	"gopkg.in/urfave/cli.v1"
)

var (
	blockPrefix         = []byte("b") // (prefix, block id) -> block
	txMetaPrefix        = []byte("t") // (prefix, tx id) -> tx location
	blockReceiptsPrefix = []byte("r") // (prefix, block id) -> receipts

	bestBlockKey = []byte("best")
	bestQCKey    = []byte("best-qc")
	// added for new flattern index schema
	hashKeyPrefix = []byte("hash") // (prefix, block num) -> block hash

	version   string
	gitCommit string
	gitTag    string
	flags     = []cli.Flag{dataDirFlag, revisionFlag}

	defaultTxPoolOptions = txpool.Options{
		Limit:           200000,
		LimitPerAccount: 1024, /*16,*/ //XXX: increase to 1024 from 16 during the testing
		MaxLifetime:     20 * time.Minute,
	}
)

const (
	statePruningBatch = 1024
	indexPruningBatch = 512
	GCInterval        = 20 * 60 * 1000 // 20 min in milliseconds
)

func main() {
	go func() {
		fmt.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	app := cli.App{
		Version:   fullVersion(),
		Name:      "MeterDB",
		Usage:     "Node of Meter.io",
		Copyright: "2020 Meter Foundation <https://types.io/>",
		Flags:     flags,
		Action:    defaultAction,
		Commands: []cli.Command{
			// Read-only info
			{Name: "key", Usage: "generate key from string", Flags: []cli.Flag{rawFlag}, Action: genKeyAction},
			{Name: "raw", Usage: "Load raw value from database with key", Flags: []cli.Flag{dataDirFlag, keyFlag}, Action: loadRawAction},
			{Name: "block", Usage: "Load block from database on revision", Flags: []cli.Flag{dataDirFlag, revisionFlag}, Action: loadBlockAction},
			{Name: "hash", Usage: "Load block hash with block number", Flags: []cli.Flag{dataDirFlag, heightFlag}, Action: loadHashAction},
			{Name: "peek", Usage: "Load pointers like best-qc, best-block from database", Flags: []cli.Flag{dataDirFlag}, Action: peekAction},
			{Name: "stash", Usage: "Load all txs from tx.stash", Flags: []cli.Flag{dataDirFlag}, Action: loadStashAction},
			{Name: "report-db", Usage: "Scan and give total size separately for blocks/txmetas/receipts/index", Flags: []cli.Flag{dataDirFlag}, Action: reportDBAction},
			// LevelDB operations

			// DANGER! USE THIS ONLY IF YOU KNOW THE DETAILS
			// database fix
			{
				Name:   "unsafe-delete-raw",
				Usage:  "Delete raw key from local db",
				Flags:  []cli.Flag{dataDirFlag, keyFlag},
				Action: unsafeDeleteRawAction,
			},
			{
				Name:   "unsafe-set-raw",
				Usage:  "Set raw key/value to local db",
				Flags:  []cli.Flag{dataDirFlag, keyFlag, valueFlag},
				Action: unsafeSetRawAction,
			},
			{
				Name:   "local-reset",
				Usage:  "Reset chain with local highest block",
				Flags:  []cli.Flag{dataDirFlag},
				Action: safeResetAction,
			},

			{Name: "delete-block", Usage: "delete blocks", Flags: []cli.Flag{dataDirFlag, fromFlag, toFlag}, Action: runDeleteBlockAction},
			{
				Name:   "accumulated-receipt-size",
				Usage:  "Get accumulated block receipt size in range [from,to]",
				Flags:  []cli.Flag{dataDirFlag, fromFlag, toFlag},
				Action: runAccumulatedReceiptSize,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Println("ERROR: ", err)
		os.Exit(1)
	}
}

func defaultAction(ctx *cli.Context) error {
	fmt.Println("default action for mdb")
	return nil
}

type BlockInfo struct {
	Number int64  `json:"number"`
	Id     string `json:"id"`
}

type QCInfo struct {
	QcHeight int64  `json:"qcHeight"`
	Raw      string `json:"raw"`
}

func unsafeDeleteRawAction(ctx *cli.Context) error {

	mainDB, _ := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	key := ctx.String(keyFlag.Name)
	parsedKey, err := hex.DecodeString(strings.Replace(key, "0x", "", 1))
	if err != nil {
		slog.Error("could not decode hex key", "err", err)
		return nil
	}
	err = mainDB.Delete(parsedKey)
	if err != nil {
		slog.Error("could not delete key in database", "err", err, "key", key)
		return nil
	}
	slog.Info("Deleted key from db", "key", hex.EncodeToString(parsedKey))
	return nil
}

func unsafeSetRawAction(ctx *cli.Context) error {

	mainDB, _ := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	key := ctx.String(keyFlag.Name)
	parsedKey, err := hex.DecodeString(strings.Replace(key, "0x", "", 1))
	if err != nil {
		slog.Error("could not decode hex key", "err", err)
		return nil
	}
	val := ctx.String(valueFlag.Name)
	parsedVal, err := hex.DecodeString(strings.Replace(val, "0x", "", 1))

	err = mainDB.Put(parsedKey, parsedVal)
	if err != nil {
		slog.Error("could not set key in database", "err", err, "key", key)
		return nil
	}
	slog.Info("Set key/value in db", "key", hex.EncodeToString(parsedKey), "value", hex.EncodeToString(parsedVal))
	return nil
}

func reportDBAction(ctx *cli.Context) error {
	mainDB, gene := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	meterChain := initChain(gene, mainDB)
	bestBlock := meterChain.BestBlock()
	blkKeySize := 0
	blkSize := 0
	metaKeySize := 0
	metaSize := 0
	receiptKeySize := 0
	receiptSize := 0
	hashKeySize := 0
	hashSize := 0

	for i := uint32(1); i < bestBlock.Number(); i++ {
		b, _ := meterChain.GetTrunkBlock(i)
		blkKey := append(blockPrefix, b.ID().Bytes()...)
		blkKeySize += len(blkKey)
		blk, err := mainDB.Get(blkKey)
		if err != nil {
			panic(err)
		}
		blkSize += len(blk)

		for _, tx := range b.Txs {
			metaKey := append(txMetaPrefix, tx.Hash()...)
			metaKeySize += len(metaKey)
			meta, err := mainDB.Get(metaKey)
			if err != nil {
				panic(err)
			}
			metaSize += len(meta)
		}

		receiptKey := append(blockReceiptsPrefix, b.ID().Bytes()...)
		receiptKeySize += len(receiptKey)
		receipt, err := mainDB.Get(receiptKey)
		if err != nil {
			panic(err)
		}
		receiptSize += len(receipt)

		hashKey := append(hashKeyPrefix, numberAsKey(b.Number())...)
		hashKeySize += len(hashKey)
		hash, err := mainDB.Get(hashKey)
		if err != nil {
			panic(err)
		}
		hashSize += len(hash)
		if i%1000 == 0 {
			slog.Info("Current", "i", i, "blockTotalSize", blkKeySize+blkSize, "metaTotalSize", metaKeySize+metaSize, "receiptTotalSize", receiptKeySize+receiptSize, "hashTotalSize", hashKeySize+hashSize)
		}

	}
	slog.Info("Final", "num", bestBlock.Number(), "blkKeyTotalSize", blkKeySize, "blockTotalSize", blkKeySize+blkSize, "metaTotalSize", metaKeySize+metaSize, "receiptTotalSize", receiptKeySize+receiptSize, "hashTotalSize", hashKeySize+hashSize)

	return nil

}

func safeResetAction(ctx *cli.Context) error {
	mainDB, gene := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	meterChain := initChain(gene, mainDB)

	var (
		step = 1000000
	)

	cur := meterChain.BestBlock().Number()
	for step >= 1 {
		b, err := meterChain.GetTrunkBlock(cur + uint32(step))
		if err != nil {
			step = int(math.Floor(float64(step) / 2))
			slog.Info("block not exist, cut step", "num", cur+uint32(step), "newStep", step)
		} else {
			cur = b.Number()
			slog.Info("block exists, move cur", "num", cur)
		}
	}
	slog.Info("Local Best Block Number:", "num", cur)
	localBest, err := meterChain.GetTrunkBlock(cur)
	if err != nil {
		slog.Error("could not load local best block")
		slog.Error(err.Error())
		return err
	}
	slog.Info("Local Best Block ", "num", localBest.Number(), "id", localBest.ID())
	updateBest(mainDB, hex.EncodeToString(localBest.ID().Bytes()), false)
	rawQC, _ := rlp.EncodeToBytes(localBest.QC)
	updateBestQC(mainDB, hex.EncodeToString(rawQC), false)
	return nil
}

func runDeleteBlockAction(ctx *cli.Context) error {
	mainDB, _ := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	from := ctx.Uint64(fromFlag.Name)
	to := ctx.Uint64(toFlag.Name)
	for i := uint32(from); i <= uint32(to); i++ {
		numKey := numberAsKey(i)

		err := mainDB.Delete(append(hashKeyPrefix, numKey...))
		slog.Info("delete block", "num", i, "err", err)
	}

	return nil
}

type Account struct {
	pk   *ecdsa.PrivateKey
	addr common.Address
}

func runAccumulatedReceiptSize(ctx *cli.Context) error {
	mainDB, _ := openMainDB(ctx)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	from := ctx.Uint64(fromFlag.Name)
	to := ctx.Uint64(toFlag.Name)
	receiptSize := uint64(0)
	hashSize := uint64(0)
	for i := uint32(from); i <= uint32(to); i++ {
		numKey := numberAsKey(i)

		blockHash, err := mainDB.Get(append(hashKeyPrefix, numKey...))
		if err != nil {
			fmt.Println("could not get hash for ", i)
			continue
		}
		hashSize += uint64(len(numKey) + len(hashKeyPrefix) + len(blockHash))
		receiptRaw, err := mainDB.Get(append(blockReceiptsPrefix, blockHash...))
		if err != nil {
			fmt.Println("could not get receipt for ", i)
			continue
		}
		receiptSize += uint64(len(blockHash) + len(blockReceiptsPrefix) + len(receiptRaw))
		if i%100000 == 0 {
			slog.Info("still calculating accumulated receipt size", "totalReceiptSize", receiptSize, "totalHashSize", hashSize, "from", from, "i", i)
		}
	}

	slog.Info("Finished calculating accumulated receipt size", "totalReceiptSize", receiptSize, "totalHashSize", hashSize, "from", from, "to", to)

	return nil
}
