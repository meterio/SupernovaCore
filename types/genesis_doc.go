package types

type GenesisDoc struct {
	Name       string       `json:"name"`
	ChainId    uint64       `json:"chain_id"`
	Time       uint64       `json:"time"`
	Validators []*Validator `json:"validators"`
}
