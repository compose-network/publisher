// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

contract L2OutputOracle {
    /// @notice Emitted when a new L2 output is proposed.
    /// @param _block The proposed L2 output.
    event OutputProposed(SuperBlock _block);

    /// @notice Struct representing an L2 block.
    struct L2Block {
        /// @notice The slot number of this L2 block.
        uint64 slot;
        /// @notice The chain ID of the L2 chain.
        bytes chainId;
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

    /// @notice Dummy propose function that emits the proposed superblock.
    /// @param _block The superblock containing the L2 output to be proposed.
    function proposeL2Output(
        SuperBlock calldata _block
    ) external {
        emit OutputProposed(_block);
    }
}
