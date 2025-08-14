// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

// handles batch da, proof submission, and multi-chain proposals (all in one for simplicity)
contract SuperblockVerifier {
    event BatchSubmitted(uint256[] chainIds, bytes32[] outputRoots, bytes[][] includedXTs, uint256 timestamp);
    event ProofSubmitted(bytes32 superblockRoot, bytes proof, bytes[][] includedXTs, uint256 timestamp);
    event OutputProposed(uint256 chainId, bytes32 outputRoot, uint256 l2BlockNumber, bytes proof);

    bytes32 public constant GENESIS_HASH = bytes32(0); // genesis superblock hash
    bytes32 public lastSuperblockHash = GENESIS_HASH;
    uint256 public nextSuperblockNumber = 1;
    mapping(uint256 => bytes32) public superblocks; // superblock number -> superblock root

    // store latest batch output root per chain (for minimal validation)
    mapping(uint256 => bytes32) public latestBatchRoot; // chainid -> latest output root from submit batch

    // emits to simulate da posting per sequencer
    function submitBatch(
        uint256[] calldata _chainIds,
        bytes32[] calldata _outputRoots,
        bytes[][] calldata _includedXTs
    ) external {
        require(_includedXTs.length == _chainIds.length, "xts mismatch");

        // store latest output roots per chain
        for (uint256 i = 0; i < _chainIds.length; i++) {
            latestBatchRoot[_chainIds[i]] = _outputRoots[i];
        }

        emit BatchSubmitted(_chainIds, _outputRoots, _includedXTs, block.timestamp);
    }

    // emits proof, loops proposals per chain (aggregates + verifies mock superblock)
    function submitProofAndPropose(
        bytes32 _superblockRoot,
        bytes calldata _proof,
        uint256[] calldata _chainIds,
        uint256[] calldata _l2BlockNumbers,
        bytes[][] calldata _includedXTs
    ) external {
        require(_chainIds.length == _l2BlockNumbers.length, "array mismatch");
        require(_includedXTs.length == _chainIds.length, "xts mismatch");

        // TODO minimal validation, can be uncommented to check non-happy flows
        /**
        bytes32[] memory storedRoots = new bytes32[](_chainIds.length);
        for (uint256 i = 0; i < _chainIds.length; i++) {
            storedRoots[i] = latestBatchRoot[_chainIds[i]];
            require(storedRoots[i] != bytes32(0), "no batch submitted for chain");
        }
        bytes32 expectedRoot = keccak256(abi.encode(storedRoots));
        require(_superblockRoot == expectedRoot, "invalid superblock root");
        */

        emit ProofSubmitted(_superblockRoot, _proof, _includedXTs, block.timestamp);

        // simulate multi-chain proposals: loop over chains
        for (uint256 i = 0; i < _chainIds.length; i++) {
            emit OutputProposed(_chainIds[i], _superblockRoot, _l2BlockNumbers[i], _proof);
        }

        // update superblock state
        superblocks[nextSuperblockNumber] = _superblockRoot;
        lastSuperblockHash = _superblockRoot;
        nextSuperblockNumber++;

        // clear stored roots after successful propose (for poc, to reset for next slot)
        for (uint256 i = 0; i < _chainIds.length; i++) {
            delete latestBatchRoot[_chainIds[i]];
        }
    }
}
