package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/triedb"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: genesis-hash <genesis.json>")
	}

	path := os.Args[1]
	file, err := os.Open(path)
	if err != nil {
		log.Fatalf("open genesis: %v", err)
	}
	defer file.Close()

	var g core.Genesis
	if err := json.NewDecoder(file).Decode(&g); err != nil {
		log.Fatalf("decode genesis: %v", err)
	}

	db := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	defer trieDB.Close()

	block, err := g.Commit(db, trieDB)
	if err != nil {
		log.Fatalf("commit genesis: %v", err)
	}

	fmt.Printf("0x%x", block.Hash())
}
