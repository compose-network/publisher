// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script } from "forge-std/Script.sol";
import { DeployAll } from "./DeployAll.sol";

contract DeployAllRollup is Script, DeployAll {
    function run() external {
        if (block.chainid != 22222) {
            revert("This script is only for Rollup B with L1 Hoodi");
        }

        string memory finalJson = _deployAll();

        vm.writeJson(finalJson, "artifacts/deploy-rollup-b.json");
    }
}
