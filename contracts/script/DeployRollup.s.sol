// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script, console } from "forge-std/Script.sol";
import { DeployAll } from "./DeployAll.sol";

contract DeployAllRollup is Script, DeployAll {
    function run() external {
        if (block.chainid != 11111) {
            revert("This script is only for Ayaz Rollup with L1 Sepolia");
        }

        string memory finalJson = _deployAll(
            vm.readFile("script/config/rollup.json")
        );

        vm.writeJson(finalJson, "artifacts/deploy-rollup.json");
    }
}
