// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";

import {ComposeL2OutputOracle} from "../src/ComposeL2OutputOracle.sol";

contract DeployComposeL2OutputOracle is Script {
    function run() public returns (address composeL2OutputOracleImpl) {
        // Load deployer private key from environment
        uint256 deployPk = vm.envOr("DEPLOYER_PRIVATE_KEY", uint256(0));
        
        // Start broadcasting transactions
        if (deployPk != uint256(0)) {
            vm.startBroadcast(deployPk);
        } else {
            vm.startBroadcast();
        }

        // Deploy the implementation contract
        console.log("Deploying ComposeL2OutputOracle implementation...");
        composeL2OutputOracleImpl = address(new ComposeL2OutputOracle());
        
        console.log("ComposeL2OutputOracle implementation deployed at:", composeL2OutputOracleImpl);
        
        // Stop broadcasting
        vm.stopBroadcast();
    }
}
