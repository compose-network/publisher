package contracts

import (
	"math/big"
)

// superblockAggregationOutputs matches the Solidity struct in ComposeL2OutputOracle.sol
type superblockAggregationOutputs struct {
	SuperblockNumber          *big.Int         `abi:"superblockNumber"`
	ParentSuperblockBatchHash [32]byte         `abi:"parentSuperblockBatchHash"`
	BootInfo                  []bootInfoStruct `abi:"bootInfo"`
}

// bootInfoStruct matches the Solidity struct in ComposeL2OutputOracle.sol
type bootInfoStruct struct {
	L1Head           [32]byte `abi:"l1Head"`
	L2PreRoot        [32]byte `abi:"l2PreRoot"`
	L2PostRoot       [32]byte `abi:"l2PostRoot"`
	L2BlockNumber    uint64   `abi:"l2BlockNumber"`
	RollupConfigHash [32]byte `abi:"rollupConfigHash"`
}
