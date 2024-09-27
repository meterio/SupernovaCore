package main

import (
	"encoding/hex"
	"fmt"

	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

func main() {
	for i := 0; i < 10; i++ {
		secret, _ := bls.RandKey()
		fmt.Println(hex.EncodeToString(secret.Marshal()))
	}
}
