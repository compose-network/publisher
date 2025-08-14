// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import "forge-std/Script.sol";
import {SuperblockVerifier} from "../src/OPSuccinct-draft/SuperblockVerifier.sol";

contract DeploySBVerifier is Script {
    function run() external {
        vm.startBroadcast();

        SuperblockVerifier verifier = new SuperblockVerifier();
        console.log("SBVerifier deployed at:", address(verifier));

        vm.stopBroadcast();
    }
}
