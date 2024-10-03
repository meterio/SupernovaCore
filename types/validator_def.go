package types

import "fmt"

type ValidatorDef struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	PubKey  string `json:"pubkey"`
	IP      string `json:"ip"`
	Port    uint32 `json:"port"`
}

func (d ValidatorDef) String() string {
	return fmt.Sprintf("Name:%v, Address:%v, PubKey:%v, IP:%v, Port:%v", d.Name, d.Address, d.PubKey, d.IP, d.Port)
}
