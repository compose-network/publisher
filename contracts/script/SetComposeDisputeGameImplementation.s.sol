// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";

import {ComposeDisputeGame} from "../src/ComposeDisputeGame.sol";

// Minimal interfaces needed
type GameType is uint32;

interface IDisputeGame {
    function gameType() external view returns (GameType);
}

interface IDisputeGameFactory {
    function setImplementation(GameType _gameType, IDisputeGame _impl) external;
    function setInitBond(GameType _gameType, uint256 _initBond) external;
    function owner() external view returns (address);
}

contract SetComposeDisputeGameImplementation is Script {
    function run() public {
        // Environment variables
        address disputeGameFactory = vm.envAddress("DISPUTE_GAME_FACTORY_ADDRESS");
        address composeDisputeGameImpl = vm.envAddress("COMPOSE_DISPUTE_GAME_IMPL_ADDRESS");
        uint256 deployPk = vm.envOr("DEPLOYER_PRIVATE_KEY", uint256(0));
        
        console.log("DisputeGameFactory:", disputeGameFactory);
        console.log("ComposeDisputeGame Implementation:", composeDisputeGameImpl);
        console.log("Current factory owner:", IDisputeGameFactory(disputeGameFactory).owner());
        
        // Start broadcasting transactions
        if (deployPk != uint256(0)) {
            vm.startBroadcast(deployPk);
        } else {
            vm.startBroadcast();
        }

        // Set the implementation for game type 5555 (COMPOSE_GAME_TYPE)
        GameType composeGameType = GameType.wrap(5555);
        
        console.log("Setting implementation for game type:", GameType.unwrap(composeGameType));
        IDisputeGameFactory(disputeGameFactory).setImplementation(
            composeGameType,
            IDisputeGame(composeDisputeGameImpl)
        );
        
        console.log("✅ Implementation set successfully!");
        
        // Optionally set init bond to 0 for validity games (as shown in the example)
        console.log("Setting init bond to 0 for game type:", GameType.unwrap(composeGameType));
        IDisputeGameFactory(disputeGameFactory).setInitBond(composeGameType, 0);
        
        console.log("✅ Init bond set successfully!");

        vm.stopBroadcast();
    }
}

// Alternative version that also deploys the implementation first
contract DeployAndSetComposeDisputeGame is Script {
    function run() public returns (address composeDisputeGameImpl) {
        // Environment variables
        address disputeGameFactory = vm.envAddress("DISPUTE_GAME_FACTORY_ADDRESS");
        uint256 deployPk = vm.envOr("DEPLOYER_PRIVATE_KEY", uint256(0));
        
        console.log("DisputeGameFactory:", disputeGameFactory);
        console.log("Current factory owner:", IDisputeGameFactory(disputeGameFactory).owner());
        
        // Start broadcasting transactions
        if (deployPk != uint256(0)) {
            vm.startBroadcast(deployPk);
        } else {
            vm.startBroadcast();
        }

        // 1. Deploy the implementation
        console.log("Deploying ComposeDisputeGame implementation...");
        composeDisputeGameImpl = address(new ComposeDisputeGame());
        console.log("ComposeDisputeGame implementation deployed at:", composeDisputeGameImpl);

        // 2. Set the implementation for game type 5555 (COMPOSE_GAME_TYPE)
        GameType composeGameType = GameType.wrap(5555);
        
        console.log("Setting implementation for game type:", GameType.unwrap(composeGameType));
        IDisputeGameFactory(disputeGameFactory).setImplementation(
            composeGameType,
            IDisputeGame(composeDisputeGameImpl)
        );
        
        console.log("✅ Implementation set successfully!");
        
        // 3. Set init bond to 0 for validity games
        console.log("Setting init bond to 0 for game type:", GameType.unwrap(composeGameType));
        IDisputeGameFactory(disputeGameFactory).setInitBond(composeGameType, 0);
        
        console.log("✅ Init bond set successfully!");

        vm.stopBroadcast();
        
        return composeDisputeGameImpl;
    }
}
