// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script } from "forge-std/Script.sol";
import { DeployAll } from "./DeployAll.sol";

contract DeployAllRollupB is Script, DeployAll {
    function run() external {
        uint256 expectedChainId = vm.envOr("ROLLUP_B_CHAIN_ID", uint256(22222));
        if (block.chainid != expectedChainId) {
            revert("Incorrect Rollup B chain ID");
        }

        string memory finalJson = _deployAll();

        vm.writeJson(finalJson, "artifacts/deploy-rollup-b.json");
    }
}
