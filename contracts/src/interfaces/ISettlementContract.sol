// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

interface ISettlementContract {
    /// @notice Struct representing an L2 block.
    struct L2Block {
        /// @notice The slot number of this L2 block.
        uint64 slot;
        /// @notice The chain ID of the L2 chain.
        uint256 chainId;
        /// @notice The block number of this L2 block.
        uint64 blockNumber;
        /// @notice The hash of this L2 block.
        bytes blockHash;
        /// @notice The hash of the parent L2 block.
        bytes parentBlockHash;
        /// @notice The list of transactions included in this L2 block.
        bytes[] included_xts;
        /// @notice The raw block data of this L2 block.
        bytes block;
    }

    /// @notice Struct representing a superblock containing multiple L2 blocks.
    struct SuperBlock {
        /// @notice The block number of the superblock.
        uint64 blockNumber;
        /// @notice The slot number of the superblock.
        uint64 slot;
        /// @notice The hash of the parent superblock.
        bytes parentBlockHash;
        /// @notice The Merkle root of the superblock.
        bytes merkleRoot;
        /// @notice The timestamp when the superblock was created.
        uint256 timestamp;
        /// @notice The list of L2 blocks included in this superblock.
        L2Block[] l2Blocks;
        /// @notice The list of all transactions included in the L2 blocks.
        bytes[] _includedXTs;
        /// @notice The concatenated hashes of all transactions included in the L2 blocks.
        bytes _transactionHashes;
        /// @notice The status of the superblock.
        uint8 status;
    }

    struct RollupState {
        /// @notice Latest finalized block hash
        bytes32 lastFinalizedHash;
        /// @notice Latest finalized block number
        uint256 lastBlockNumber;
        /// @notice Rollup registration flag
        bool exists;
    }

    /**
     * @notice Emitted when a new rollup is registered
     *
     * @param rollupId The chain id of the newly registered rollup
     */
    event RollupRegistered(uint256 indexed rollupId);

    /**
     * @notice Emitted when a block of the rollup is finalized
     *
     * @param rollupId The chain id of the rollup
     * @param blockNumber The number of the new block
     * @param blockHash The hash of the new block
     * @param mockProof The proof for the new block
     */
    event BlockFinalized(uint256 indexed rollupId, uint256 blockNumber, bytes32 blockHash, bytes32 mockProof);

    /**
     * @notice Emitted when a super block state is finalized
     *
     * @param blockNumber The number of the super block
     * @param blockHash The hash of the super block
     * @param merkleRoot The merkle root of the super block
     */
    event SuperBlockFinalized(uint256 blockNumber, bytes32 blockHash, bytes32 merkleRoot);

    /**
     * @notice Thrown when trying to register already existing rollup
     */
    error RollupAlreadyExists();

    /**
     * @notice Thrown when truing to submit a super block non-sequentially
     */
    error InvalidBlockNumber();

    /**
     * @notice Thrown when trying to access non-registered rollup
     */
    error RollupNotRegistered();
}