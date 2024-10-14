package main

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	_ "net/http/pprof"

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
	QcHeight int64  `json:"height"`
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
