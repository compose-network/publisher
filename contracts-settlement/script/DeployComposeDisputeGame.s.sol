// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";

import {ComposeDisputeGame} from "../src/ComposeDisputeGame.sol";

contract DeployComposeDisputeGame is Script {
    function run() public returns (address composeDisputeGame) {
        // Load deployer private key from environment
        uint256 deployPk = vm.envOr("DEPLOYER_PRIVATE_KEY", uint256(0));
        
        // Start broadcasting transactions
        if (deployPk != uint256(0)) {
            vm.startBroadcast(deployPk);
        } else {
            vm.startBroadcast();
        }

        // Deploy the implementation contract
        console.log("Deploying ComposeDisputeGame implementation...");
        composeDisputeGame = address(new ComposeDisputeGame());
        
        console.log("ComposeDisputeGame implementation deployed at:", composeDisputeGame);
        
        // Stop broadcasting
        vm.stopBroadcast();
    }
}
