// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package chain

import (
	"encoding/binary"
	"encoding/json"

	db "github.com/cometbft/cometbft-db"
	v1 "github.com/cometbft/cometbft/api/cometbft/abci/v1"
	cmtproto "github.com/cometbft/cometbft/api/cometbft/types/v1"
	cmttypes "github.com/cometbft/cometbft/types"
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

func saveRLP(w db.Batch, key []byte, val interface{}) error {
	data, err := rlp.EncodeToBytes(val)
	if err != nil {
		return err
	}
	return w.Set(key, data)
}

func loadRLP(r db.DB, key []byte, val interface{}) error {
	data, err := r.Get(key)
	if err != nil {
		return err
	}
	return rlp.DecodeBytes(data, val)
}

func saveJSON(w db.Batch, key []byte, val interface{}) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return w.Set(key, data)
}

func loadJSON(r db.DB, key []byte, val interface{}) error {
	data, err := r.Get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &val)
}

// loadBestBlockID returns the best block ID on trunk.
func loadBestBlockID(r db.DB) (types.Bytes32, error) {
	data, err := r.Get(bestBlockKey)
	if err != nil || data == nil {
		return types.Bytes32{}, err
	}
	return types.BytesToBytes32(data), nil
}

// saveBestBlockID save the best block ID on trunk.
func batchSaveBestBlockID(w db.Batch, id types.Bytes32) error {
	return w.Set(bestBlockKey, id[:])
}

// saveBestBlockID save the best block ID on trunk.
func saveBestBlockID(w db.DB, id types.Bytes32) error {
	return w.Set(bestBlockKey, id[:])
}

func deleteBlockHash(w db.Batch, num uint32) error {
	numKey := numberAsKey(num)
	return w.Delete(append(hashKeyPrefix, numKey...))
}

// loadBlockHash returns the block hash on trunk with num.
func loadBlockHash(r db.DB, num uint32) (types.Bytes32, error) {
	numKey := numberAsKey(num)
	data, err := r.Get(append(hashKeyPrefix, numKey...))
	if err != nil {
		return types.Bytes32{}, err
	}
	return types.BytesToBytes32(data), nil
}

// saveBlockHash save the block hash on trunk corresponding to a num.
func saveBlockHash(w db.Batch, num uint32, id types.Bytes32) error {
	numKey := numberAsKey(num)
	return w.Set(append(hashKeyPrefix, numKey...), id[:])
}

// loadBlockRaw load rlp encoded block raw data.
func loadBlockRaw(r db.DB, id types.Bytes32) (block.Raw, error) {
	return r.Get(append(blockPrefix, id[:]...))
}

func removeBlockRaw(w db.Batch, id types.Bytes32) error {
	return w.Delete(append(blockPrefix, id[:]...))
}

// saveBlockRaw save rlp encoded block raw data.
func saveBlockRaw(w db.Batch, id types.Bytes32, raw block.Raw) error {
	return w.Set(append(blockPrefix, id[:]...), raw)
}

func deleteBlockRaw(w db.DB, id types.Bytes32) error {
	return w.Delete(append(blockPrefix, id[:]...))
}

// saveBlockNumberIndexTrieRoot save the root of trie that contains number to id index.
func saveBlockNumberIndexTrieRoot(w db.Batch, id types.Bytes32, root types.Bytes32) error {
	return w.Set(append(indexTrieRootPrefix, id[:]...), root[:])
}

// loadBlockNumberIndexTrieRoot load trie root.
func loadBlockNumberIndexTrieRoot(r db.DB, id types.Bytes32) (types.Bytes32, error) {
	root, err := r.Get(append(indexTrieRootPrefix, id[:]...))
	if err != nil {
		return types.Bytes32{}, err
	}
	return types.BytesToBytes32(root), nil
}

// saveTxMeta save locations of a tx.
func saveTxMeta(w db.Batch, txID []byte, meta []TxMeta) error {
	return saveRLP(w, append(txMetaPrefix, txID[:]...), meta)
}

func deleteTxMeta(w db.DB, txID []byte) error {
	return w.Delete(append(txMetaPrefix, txID[:]...))
}

// loadTxMeta load tx meta info by tx id.
func hasTxMeta(r db.DB, txID []byte) (bool, error) {
	return r.Has(append(txMetaPrefix, txID[:]...))
}

// loadTxMeta load tx meta info by tx id.
func loadTxMeta(r db.DB, txID []byte) ([]TxMeta, error) {
	var meta []TxMeta
	if err := loadRLP(r, append(txMetaPrefix, txID[:]...), &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func deleteBlock(rw db.DB, blockID types.Bytes32) (*block.Block, error) {
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
func saveBestQC(w db.DB, qc *block.QuorumCert) error {
	bestQCHeightGauge.Set(float64(qc.Number()))
	batch := w.NewBatch()
	saveRLP(batch, bestQCKey, qc)
	return batch.Write()
}

func batchSaveBestQC(w db.Batch, qc *block.QuorumCert) error {
	return saveRLP(w, bestQCKey, qc)
}

// loadBestQC load the best qc
func loadBestQC(r db.DB) (*block.QuorumCert, error) {
	var qc block.QuorumCert
	if err := loadRLP(r, bestQCKey, &qc); err != nil {
		return nil, err
	}
	return &qc, nil
}

// saveBestQC save the best qc
func saveValidatorSet(w db.DB, vset *cmttypes.ValidatorSet) error {
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
func loadValidatorSet(r db.DB, vhash []byte) (*cmttypes.ValidatorSet, error) {
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
func saveInitChainResponse(w db.DB, res *v1.InitChainResponse) error {
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
func loadInitChainResponse(r db.DB) (*v1.InitChainResponse, error) {
	res := new(v1.InitChainResponse)
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
func saveFinalizeBlockResponse(w db.DB, blockID types.Bytes32, res *v1.FinalizeBlockResponse) error {
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
func loadFinalizeBlockResponse(r db.DB, blockID types.Bytes32) (*v1.FinalizeBlockResponse, error) {
	res := new(v1.FinalizeBlockResponse)
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
