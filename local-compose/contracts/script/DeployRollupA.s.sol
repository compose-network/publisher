// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script } from "forge-std/Script.sol";
import { DeployAll } from "./DeployAll.sol";

contract DeployAllRollup is Script, DeployAll {
    function run() external {
        uint256 expectedChainId = vm.envOr("ROLLUP_A_CHAIN_ID", uint256(11111));
        if (block.chainid != expectedChainId) {
            revert("Incorrect Rollup A chain ID");
        }

        string memory finalJson = _deployAll();

        vm.writeJson(finalJson, "artifacts/deploy-rollup-a.json");
    }
}
