// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { ISettlementContract } from "../interfaces/ISettlementContract.sol";

contract SettlementContract is Ownable, ISettlementContract {
    /// @notice The mapping of rollup states at each block
    mapping(uint256 chainId => RollupState stateStruct) public rollupStates;
    /// @notice The mapping of super chain states at each block
    mapping(uint256 blockNumber => SuperBlock blockData) public superBlocks;

    /**
     * @notice Constructs the contract, sets the owner address and genesis block
     *
     * @param genesisBlock The starting block of the super chain
     */
    constructor(SuperBlock calldata genesisBlock) Ownable(msg.sender) {
        superBlocks[0] = genesisBlock;
    }

    /**
     * @notice Registers a new rollup
     *
     * @dev Only callable by the contract owner
     */
    function registerRollup(uint256 rollupId) external onlyOwner {
        // TODO use access control roles or store sequencer address on the contract ?

        if (rollupStates[rollupId].exists) {
            revert RollupAlreadyExists();
        }

        rollupStates[rollupId] = RollupState({
            lastFinalizedHash: bytes32(0),
            lastBlockNumber: 0,
            exists: true
        });

        emit RollupRegistered(rollupId);
    }

    function submitAndFinalizeBlocks(SuperBlock calldata superBlock) external onlyOwner {
        SuperBlock storage previousBlock = superBlocks[superBlock.blockNumber - 1];

        if (previousBlock.blockNumber == 0) {
            revert InvalidBlockNumber();
        }

        // TODO add previous block hash check vs new parentBlockHash ?

        for (uint256 i; i< superBlock.l2Blocks.length; i++) {
            L2Block storage l2Block;

            if (!rollupStates[l2Block.chainId].exists) {
                revert RollupNotRegistered();
            }

            // TODO add checks for previous block number ?

            rollupStates[l2Block.chainId].lastFinalizedHash = l2Block.chainId;
            rollupStates[l2Block.chainId].lastBlockNumber = l2Block.blockNumber;
        }
    }

    function getFinalizedState(uint256 rollupId) external view returns (bytes32 hash, uint256 blockNumber) {
        RollupState storage state = rollupStates[rollupId];
        if (!state.exists) {
            revert RollupNotRegistered();
        }
        return (state.lastFinalizedHash, state.lastBlockNumber);
    }
}