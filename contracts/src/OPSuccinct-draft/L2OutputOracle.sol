// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

contract L2OutputOracle {
    event OutputProposed(uint256 chainId, bytes32 outputRoot, uint256 l2BlockNumber, bytes proof, bytes[] includedXTs);

    // no checks, just emit (dummy for poc)
    function proposeL2Output(
        bytes32 _outputRoot,
        uint256 _l2BlockNumber,
        bytes calldata _proof,
        uint256 _chainId,  // added for multi-chain thing
        bytes[] calldata _includedXTs
    ) external {
        emit OutputProposed(_chainId, _outputRoot, _l2BlockNumber, _proof, _includedXTs);
    }
}
