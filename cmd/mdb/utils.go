// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	db "github.com/cosmos/cosmos-db"
	"github.com/meterio/supernova/libs/lvldb"
	"github.com/meterio/supernova/types"
)

func fatal(args ...interface{}) {
	var w io.Writer
	if runtime.GOOS == "windows" {
		// The SameFile check below doesn't work on Windows.
		// stdout is unlikely to get redirected though, so just print there.
		w = os.Stdout
	} else {
		outf, err := os.Stdout.Stat()
		if err != nil {
			fmt.Println("could not get os stdout, error:", err)
			panic("could not get os stdout")
		}

		errf, err := os.Stderr.Stat()
		if err != nil {
			fmt.Println("could not get os stderr, error:", err)
			panic("could not get os stderr")
		}

		if outf != nil && errf != nil && os.SameFile(outf, errf) {
			w = os.Stderr
		} else {
			w = io.MultiWriter(os.Stdout, os.Stderr)
		}
	}
	fmt.Fprint(w, "Fatal: ")
	fmt.Fprintln(w, args...)
	os.Exit(1)
}

func handleExitSignal() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		exitSignalCh := make(chan os.Signal)
		signal.Notify(exitSignalCh, os.Interrupt, os.Kill, syscall.SIGTERM)

		select {
		case sig := <-exitSignalCh:
			slog.Info("exit signal received", "signal", sig)
			cancel()
		}
	}()
	return ctx
}

// ReadTrieNode retrieves the trie node of the provided hash.
func ReadTrieNode(db db.DB, hash types.Bytes32) []byte {
	data, _ := db.Get(hash.Bytes())
	return data
}

// HasCode checks if the contract code corresponding to the
// provided code hash is present in the db.
func HasCode(db db.DB, hash types.Bytes32) bool {
	// Try with the prefixed code scheme first, if not then try with legacy
	// scheme.
	// if ok := HasCodeWithPrefix(db, hash); ok {
	// 	return true
	// }
	ok, _ := db.Has(hash.Bytes())
	return ok
}

func getJson(client *http.Client, url string, target interface{}) error {
	r, err := client.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}

func getValue(ldb *lvldb.LevelDB, key []byte) (string, error) {
	val, err := ldb.Get(key)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(val), err
}

func updateBestQC(ldb *lvldb.LevelDB, hexVal string, dryRun bool) error {
	v, _ := hex.DecodeString(hexVal)
	if !dryRun {
		ldb.Put(bestQCKey, v)
		fmt.Println("best-qc updated:", hexVal)
	} else {
		fmt.Println("best-qc will be:", hexVal)
	}
	return nil
}

func readBestQC(ldb *lvldb.LevelDB) (string, error) {
	val, err := getValue(ldb, bestQCKey)
	return val, err
}

func updateBest(ldb *lvldb.LevelDB, hexVal string, dryRun bool) error {
	v, _ := hex.DecodeString(hexVal)
	if !dryRun {
		ldb.Put(bestBlockKey, v)
		fmt.Println("best updated:", hexVal)
	} else {
		fmt.Println("best will be:", hexVal)
	}
	return nil
}

func readBest(ldb *lvldb.LevelDB) (string, error) {
	val, err := getValue(ldb, bestBlockKey)
	return val, err
}
