package types

import (
	"crypto/ecdsa"
	"os"

	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/ethereum/go-ethereum/crypto"
	cmn "github.com/meterio/supernova/libs/common"
)

// NodeKey is the persistent peer key.
// It contains the nodes private key for authentication.
type NodeKey struct {
	Bytes []byte `json:"priv_key"` // our priv key
}

func (nodeKey *NodeKey) PrivateKey() *ecdsa.PrivateKey {
	pk, err := crypto.ToECDSA(nodeKey.Bytes)
	if err != nil {
		return nil
	}
	return pk
}

// PubKey returns the peer's PubKey.
func (nodeKey *NodeKey) PubKey() ecdsa.PublicKey {
	return nodeKey.PrivateKey().PublicKey
}

// LoadOrGenNodeKey attempts to load the NodeKey from the given filePath. If
// the file does not exist, it generates and saves a new NodeKey.
func LoadOrGenNodeKey(filePath string) (*NodeKey, error) {
	if cmn.FileExists(filePath) {
		nodeKey, err := LoadNodeKey(filePath)
		if err != nil {
			return nil, err
		}
		return nodeKey, nil
	}

	privKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	nodeKey := &NodeKey{
		Bytes: crypto.FromECDSA(privKey),
	}

	if err := nodeKey.SaveAs(filePath); err != nil {
		return nil, err
	}

	return nodeKey, nil
}

// LoadNodeKey loads NodeKey located in filePath.
func LoadNodeKey(filePath string) (*NodeKey, error) {
	jsonBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	nodeKey := new(NodeKey)
	err = cmtjson.Unmarshal(jsonBytes, nodeKey)
	if err != nil {
		return nil, err
	}
	return nodeKey, nil
}

// SaveAs persists the NodeKey to filePath.
func (nodeKey *NodeKey) SaveAs(filePath string) error {
	jsonBytes, err := cmtjson.Marshal(nodeKey)
	if err != nil {
		return err
	}
	err = os.WriteFile(filePath, jsonBytes, 0o600)
	if err != nil {
		return err
	}
	return nil
}
