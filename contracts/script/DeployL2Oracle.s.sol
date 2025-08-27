// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import "forge-std/Script.sol";
import {L2OutputOracle} from "../src/OPSuccinct-draft/L2OutputOracle.sol";

contract DeployL2Oracle is Script {
    function run() external {
        vm.startBroadcast();

        L2OutputOracle oracle = new L2OutputOracle();
        console.log("L2Oracle deployed at:", address(oracle));

        vm.stopBroadcast();
    }
}
