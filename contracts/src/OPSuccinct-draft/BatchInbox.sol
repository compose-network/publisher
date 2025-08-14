// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

// inbox for submitting batch data (first sequencer call)
contract BatchInbox {
    event BatchSubmitted(uint256[] chainIds, bytes32[] outputRoots, bytes[][] includedXTs, uint256 timestamp);

    // store latest batch output root per chain (for minimal validation)
    mapping(uint256 => bytes32) public latestBatchRoot; // chainid -> latest outputroot from submitbatch

    // dummy submit batch: emits and simulates data posting
    function submitBatch(
        uint256[] calldata _chainIds,
        bytes32[] calldata _outputRoots,
        bytes[][] calldata _includedXTs
    ) external {
        require(_outputRoots.length == _chainIds.length, "roots mismatch");
        require(_includedXTs.length == _chainIds.length, "xts mismatch");

        // store latest output roots per chain
        for (uint256 i = 0; i < _chainIds.length; i++) {
            latestBatchRoot[_chainIds[i]] = _outputRoots[i];
        }

        emit BatchSubmitted(_chainIds, _outputRoots, _includedXTs, block.timestamp);
    }

    // clear latest batch root
    function clearLatestBatchRoot(uint256 _chainId) external {
        latestBatchRoot[_chainId] = bytes32(0);
    }
}
