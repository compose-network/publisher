// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script, console } from "forge-std/Script.sol";
import { DeployAll } from "./DeployAll.sol";

contract DeployAllRollup is Script, DeployAll {
    function run() external {
        if (block.chainid != 22222) {
            revert("This script is only for Ayaz Rollup with L1 Hoodi");
        }

        string memory finalJson = _deployAll(
            vm.readFile("script/config/rollup-hoodi.json")
        );

        vm.writeJson(finalJson, "artifacts/deploy-rollup-hoodi.json");
    }
}
