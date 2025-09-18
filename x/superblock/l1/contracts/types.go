package contracts

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// superblockAggregationOutputs matches the Solidity struct in ComposeL2OutputOracle.sol
type superblockAggregationOutputs struct {
	SuperblockNumber          *big.Int         `abi:"superblockNumber"`
	ParentSuperblockBatchHash common.Hash      `abi:"parentSuperblockBatchHash"`
	CommitmentHash            string           `abi:"commitmentHash"`
	BootInfo                  []bootInfoStruct `abi:"bootInfo"`
}

// bootInfoStruct matches the Solidity struct in ComposeL2OutputOracle.sol
type bootInfoStruct struct {
	L1Head           common.Hash `abi:"l1Head"`
	L2PreRoot        common.Hash `abi:"l2PreRoot"`
	L2PostRoot       common.Hash `abi:"l2PostRoot"`
	L2BlockNumber    uint64      `abi:"l2BlockNumber"`
	RollupConfigHash common.Hash `abi:"rollupConfigHash"`
}

// Helper functions for ABI type parsing
func mustParseType(typeName string, components []abi.ArgumentMarshaling) abi.Type {
	typ, err := abi.NewType(typeName, typeName, components)
	if err != nil {
		panic(fmt.Sprintf("failed to parse ABI type %s: %v", typeName, err))
	}
	return typ
}

func buildSuperblockAggregationOutputsType() []abi.ArgumentMarshaling {
	return []abi.ArgumentMarshaling{
		{Name: "superblockNumber", Type: "uint256"},
		{Name: "parentSuperblockBatchHash", Type: "bytes32"},
		{Name: "commitmentHash", Type: "bytes32"},
		{
			Name: "bootInfo",
			Type: "tuple[]",
			Components: []abi.ArgumentMarshaling{
				{Name: "l1Head", Type: "bytes32"},
				{Name: "l2PreRoot", Type: "bytes32"},
				{Name: "l2PostRoot", Type: "bytes32"},
				{Name: "l2BlockNumber", Type: "uint64"},
				{Name: "rollupConfigHash", Type: "bytes32"},
			},
		},
	}
}
