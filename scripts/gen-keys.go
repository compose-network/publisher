// Small helper to generate dev ECDSA keys (secp256k1) and print
// - private key (hex)
// - compressed public key (hex)
// - Ethereum address derived from public key
package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

func gen(label string) {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic(err)
	}
	priv := fmt.Sprintf("%x", crypto.FromECDSA(key))
	pub := fmt.Sprintf("%x", crypto.CompressPubkey(&key.PublicKey))
	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
	fmt.Printf("%s_PRIV=%s\n%s_PUB=%s\n%s_ADDR=%s\n\n", label, priv, label, pub, label, addr)
}

func main() {
	gen("SP")
	gen("SEQ1")
	gen("SEQ2")
}
