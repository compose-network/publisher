// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import "./BatchInbox.sol";
import "./L2OutputOracle.sol";

// aggregates and calls oracle (like opsuccinct)
contract Proposer {
    L2OutputOracle public oracle;
    BatchInbox public batchInbox;

    event ProofSubmitted(bytes32 superblockRoot, bytes proof, bytes[][] includedXTs, uint256 timestamp);

    bytes32 public constant GENESIS_HASH = bytes32(0); // genesis superblock hash
    bytes32 public lastSuperblockHash = GENESIS_HASH;
    uint256 public nextSuperblockNumber = 1;
    mapping(uint256 => bytes32) public superblocks; // superblock number -> superblock root

    constructor(L2OutputOracle _oracle, BatchInbox _batchInbox) {
        oracle = _oracle;
        batchInbox = _batchInbox;
    }

    // emits and calls proposeL2Output per chain (multi-chain sim)
    function submitProofAndPropose(
        bytes32 _superblockRoot,
        bytes calldata _proof,
        uint256[] calldata _chainIds,
        uint256[] calldata _l2BlockNumbers,
        bytes[][] calldata _includedXTs
    ) external {
        require(_chainIds.length == _l2BlockNumbers.length, "array mismatch");
        require(_includedXTs.length == _chainIds.length, "xts mismatch");

//        bytes32[] memory storedRoots = new bytes32[](_chainIds.length);
//        for (uint256 i = 0; i < _chainIds.length; i++) {
//            storedRoots[i] = batchInbox.latestBatchRoot(_chainIds[i]);
//            require(storedRoots[i] != bytes32(0), "no batch submitted for chain");
//        }
//        bytes32 expectedRoot = keccak256(abi.encode(storedRoots));
//        require(_superblockRoot == expectedRoot, "invalid superblock root");

        emit ProofSubmitted(_superblockRoot, _proof, _includedXTs, block.timestamp);

        // simulate multi-chain: loop over chains
        for (uint256 i = 0; i < _chainIds.length; i++) {
            oracle.proposeL2Output(_superblockRoot, _l2BlockNumbers[i], _proof, _chainIds[i], _includedXTs[i]);
        }

        // update superblock state
        superblocks[nextSuperblockNumber] = _superblockRoot;
        lastSuperblockHash = _superblockRoot;
        nextSuperblockNumber++;

        for (uint256 i = 0; i < _chainIds.length; i++) {
            batchInbox.clearLatestBatchRoot(_chainIds[i]);
        }
    }
}
