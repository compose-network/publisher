// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {ISP1Verifier} from "@sp1-contracts/src/ISP1Verifier.sol";

contract MockVerifier is ISP1Verifier {
    bool public shouldAcceptProof = false;

    function mockVerifyProof(bool _shouldAccept) external {
        shouldAcceptProof = _shouldAccept;
    }

    function verifyProof(
        bytes32 programVkey,
        bytes calldata publicValues,
        bytes calldata proof
    ) external view override {
        if (!shouldAcceptProof) {
            revert("MockVerifier: Proof verification failed");
        }
    }
}
