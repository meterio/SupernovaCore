// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package chain

import (
	"encoding/binary"
	"encoding/json"

	cmtdb "github.com/cometbft/cometbft-db"
	v2 "github.com/cometbft/cometbft/api/cometbft/abci/v2"
	cmtproto "github.com/cometbft/cometbft/api/cometbft/types/v2"
	cmttypes "github.com/cometbft/cometbft/v2/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

var (
	blockPrefix         = []byte("b")    // (prefix, block id) -> block
	txMetaPrefix        = []byte("t")    // (prefix, tx id) -> tx location
	blockReceiptsPrefix = []byte("r")    // (prefix, block id) -> receipts
	indexTrieRootPrefix = []byte("i")    // (prefix, block id) -> trie root
	validatorPrefix     = []byte("v")    // (prefix, validator set hash) -> validator set
	finalizeBlockPrefix = []byte("f")    // (prefix, block id) -> finalize block response
	initChainPrefix     = []byte("init") // (prefix) -> init chain response

	bestBlockKey = []byte("best")    // best block hash
	bestQCKey    = []byte("best-qc") // best qc raw

	// added for new flattern index schema
	hashKeyPrefix = []byte("h") // (prefix, block num) -> block hash

)

func numberAsKey(num uint32) []byte {
	var key [4]byte
	binary.BigEndian.PutUint32(key[:], num)
	return key[:]
}

// TxMeta contains information about a tx is settled.
type TxMeta struct {
	BlockID types.Bytes32

	// Index the position of the tx in block's txs.
	Index uint64 // rlp require uint64.

	Reverted bool
}

func saveRLP(w cmtdb.Batch, key []byte, val interface{}) error {
	data, err := rlp.EncodeToBytes(val)
	if err != nil {
		return err
	}
	return w.Set(key, data)
}

func loadRLP(r cmtdb.DB, key []byte, val interface{}) error {
	data, err := r.Get(key)
	if err != nil {
		return err
	}
	return rlp.DecodeBytes(data, val)
}

func saveJSON(w cmtdb.Batch, key []byte, val interface{}) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return w.Set(key, data)
}

func loadJSON(r cmtdb.DB, key []byte, val interface{}) error {
	data, err := r.Get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &val)
}

// loadBestBlockID returns the best block ID on trunk.
func loadBestBlockID(r cmtdb.DB) (types.Bytes32, error) {
	data, err := r.Get(bestBlockKey)
	if err != nil || data == nil {
		return types.Bytes32{}, err
	}
	return types.BytesToBytes32(data), nil
}

// saveBestBlockID save the best block ID on trunk.
func batchSaveBestBlockID(w cmtdb.Batch, id types.Bytes32) error {
	return w.Set(bestBlockKey, id[:])
}

// saveBestBlockID save the best block ID on trunk.
func saveBestBlockID(w cmtdb.DB, id types.Bytes32) error {
	return w.Set(bestBlockKey, id[:])
}

func deleteBlockHash(w cmtdb.Batch, num uint32) error {
	numKey := numberAsKey(num)
	return w.Delete(append(hashKeyPrefix, numKey...))
}

// loadBlockHash returns the block hash on trunk with num.
func loadBlockHash(r cmtdb.DB, num uint32) (types.Bytes32, error) {
	numKey := numberAsKey(num)
	data, err := r.Get(append(hashKeyPrefix, numKey...))
	if err != nil {
		return types.Bytes32{}, err
	}
	return types.BytesToBytes32(data), nil
}

// saveBlockHash save the block hash on trunk corresponding to a num.
func saveBlockHash(w cmtdb.Batch, num uint32, id types.Bytes32) error {
	numKey := numberAsKey(num)
	return w.Set(append(hashKeyPrefix, numKey...), id[:])
}

// loadBlockRaw load rlp encoded block raw data.
func loadBlockRaw(r cmtdb.DB, id types.Bytes32) (block.Raw, error) {
	return r.Get(append(blockPrefix, id[:]...))
}

func removeBlockRaw(w cmtdb.Batch, id types.Bytes32) error {
	return w.Delete(append(blockPrefix, id[:]...))
}

// saveBlockRaw save rlp encoded block raw data.
func saveBlockRaw(w cmtdb.Batch, id types.Bytes32, raw block.Raw) error {
	return w.Set(append(blockPrefix, id[:]...), raw)
}

func deleteBlockRaw(w cmtdb.DB, id types.Bytes32) error {
	return w.Delete(append(blockPrefix, id[:]...))
}

// saveBlockNumberIndexTrieRoot save the root of trie that contains number to id index.
func saveBlockNumberIndexTrieRoot(w cmtdb.Batch, id types.Bytes32, root types.Bytes32) error {
	return w.Set(append(indexTrieRootPrefix, id[:]...), root[:])
}

// loadBlockNumberIndexTrieRoot load trie root.
func loadBlockNumberIndexTrieRoot(r cmtdb.DB, id types.Bytes32) (types.Bytes32, error) {
	root, err := r.Get(append(indexTrieRootPrefix, id[:]...))
	if err != nil {
		return types.Bytes32{}, err
	}
	return types.BytesToBytes32(root), nil
}

// saveTxMeta save locations of a tx.
func saveTxMeta(w cmtdb.Batch, txID []byte, meta []TxMeta) error {
	return saveRLP(w, append(txMetaPrefix, txID[:]...), meta)
}

func deleteTxMeta(w cmtdb.DB, txID []byte) error {
	return w.Delete(append(txMetaPrefix, txID[:]...))
}

// loadTxMeta load tx meta info by tx id.
func hasTxMeta(r cmtdb.DB, txID []byte) (bool, error) {
	return r.Has(append(txMetaPrefix, txID[:]...))
}

// loadTxMeta load tx meta info by tx id.
func loadTxMeta(r cmtdb.DB, txID []byte) ([]TxMeta, error) {
	var meta []TxMeta
	if err := loadRLP(r, append(txMetaPrefix, txID[:]...), &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func deleteBlock(rw cmtdb.DB, blockID types.Bytes32) (*block.Block, error) {
	raw, err := loadBlockRaw(rw, blockID)
	if err != nil {
		return nil, err
	}

	blk, err := (&rawBlock{raw: raw}).Block()
	if err != nil {
		return nil, err
	}

	for _, tx := range blk.Transactions() {
		err = deleteTxMeta(rw, tx.Hash())
		if err != nil {
			return blk, err
		}
	}

	err = deleteBlockRaw(rw, blockID)
	return blk, err
}

// saveBestQC save the best qc
func saveBestQC(w cmtdb.DB, qc *block.QuorumCert) error {
	bestQCHeightGauge.Set(float64(qc.Number()))
	batch := w.NewBatch()
	saveRLP(batch, bestQCKey, qc)
	return batch.Write()
}

func batchSaveBestQC(w cmtdb.Batch, qc *block.QuorumCert) error {
	return saveRLP(w, bestQCKey, qc)
}

// loadBestQC load the best qc
func loadBestQC(r cmtdb.DB) (*block.QuorumCert, error) {
	var qc block.QuorumCert
	if err := loadRLP(r, bestQCKey, &qc); err != nil {
		return nil, err
	}
	return &qc, nil
}

// saveBestQC save the best qc
func saveValidatorSet(w cmtdb.DB, vset *cmttypes.ValidatorSet) error {
	batch := w.NewBatch()
	key := append(validatorPrefix, vset.Hash()...)

	vsetProto, err := vset.ToProto()
	if err != nil {
		return err
	}
	vsetBytes, err := vsetProto.Marshal()
	if err != nil {
		return err
	}
	err = batch.Set(key, vsetBytes)
	if err != nil {
		return err
	}
	return batch.Write()
}

// loadBestQC load the best qc
func loadValidatorSet(r cmtdb.DB, vhash []byte) (*cmttypes.ValidatorSet, error) {
	vsetProto := new(cmtproto.ValidatorSet)
	key := append(validatorPrefix, vhash...)
	vsetBytes, err := r.Get(key)
	if err != nil {
		return nil, err
	}

	vsetProto.Unmarshal(vsetBytes)
	vset, err := cmttypes.ValidatorSetFromProto(vsetProto)
	if err != nil {
		return nil, err
	}
	return vset, err
}

// saveInitChainResponse save the init chain response
func saveInitChainResponse(w cmtdb.DB, res *v2.InitChainResponse) error {
	batch := w.NewBatch()
	key := append(initChainPrefix)

	marshaled, err := res.Marshal()
	if err != nil {
		return err
	}
	err = batch.Set(key, marshaled)
	if err != nil {
		return err
	}
	return batch.Write()
}

// loadInitChainResponse load the init chain response
func loadInitChainResponse(r cmtdb.DB) (*v2.InitChainResponse, error) {
	res := new(v2.InitChainResponse)
	key := append(initChainPrefix)
	marshaled, err := r.Get(key)
	if err != nil {
		return nil, err
	}

	err = res.Unmarshal(marshaled)
	if err != nil {
		return nil, err
	}
	return res, err
}

// saveInitChainResponse save the init chain response
func saveFinalizeBlockResponse(w cmtdb.DB, blockID types.Bytes32, res *v2.FinalizeBlockResponse) error {
	batch := w.NewBatch()
	key := append(finalizeBlockPrefix, blockID[:]...)

	marshaled, err := res.Marshal()
	if err != nil {
		return err
	}
	err = batch.Set(key, marshaled)
	if err != nil {
		return err
	}
	return batch.Write()
}

// loadInitChainResponse load the init chain response
func loadFinalizeBlockResponse(r cmtdb.DB, blockID types.Bytes32) (*v2.FinalizeBlockResponse, error) {
	res := new(v2.FinalizeBlockResponse)
	key := append(finalizeBlockPrefix, blockID[:]...)
	marshaled, err := r.Get(key)
	if err != nil {
		return nil, err
	}

	err = res.Unmarshal(marshaled)
	if err != nil {
		return nil, err
	}
	return res, err
}
