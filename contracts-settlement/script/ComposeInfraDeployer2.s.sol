// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";

import {ComposeL2OutputOracle} from "../src/ComposeL2OutputOracle.sol";
import {ComposeDisputeGame} from "../src/ComposeDisputeGame.sol";

import {DisputeGameFactory} from "@optimism/src/dispute/DisputeGameFactory.sol";
import {AnchorStateRegistry} from "@optimism/src/dispute/AnchorStateRegistry.sol";

import {Proxy} from "@optimism/src/universal/Proxy.sol";
import {ProxyAdmin} from "@optimism/src/universal/ProxyAdmin.sol";

import {ISystemConfig} from "interfaces/L1/ISystemConfig.sol";
import {IDisputeGame} from "interfaces/dispute/IDisputeGame.sol";
import {GameType, Proposal, Hash} from "@optimism/src/dispute/lib/Types.sol";

contract ComposeInfraDeployer is Script {
    function run(
        address proxyAdmin,
        address l2ooProxy,
        address dgf
    ) public returns (address asrProxyAddr, address gameImplAddr) {
        uint256 deployPk = vm.envOr("DEPLOYER_PRIVATE_KEY", uint256(0));
        if (deployPk != uint256(0)) {
            vm.startBroadcast(deployPk);
        } else {
            vm.startBroadcast();
        }

        address systemConfig = vm.envAddress("SYSTEM_CONFIG_ADDR"); // TODO: used by optimism, change it
        uint256 asrFinalityDelay = vm.envOr(
            "ASR_FINALITY_DELAY_SECONDS",
            uint256(1 days)
        );

        // 1) Deploy ComposeDisputeGame implementation and register with DGF
        ComposeDisputeGame gameImpl = new ComposeDisputeGame(
            address(l2ooProxy)
        );
        DisputeGameFactory(dgf).setImplementation(
            GameType.wrap(5555),
            IDisputeGame(address(gameImpl))
        );
        // Require zero init bond for validity games
        DisputeGameFactory(dgf).setInitBond(GameType.wrap(5555), 0);
        console.log("ComposeDisputeGame (impl):", address(gameImpl));

        // 2) Deploy AnchorStateRegistry with constructor arg and behind Bedrock Proxy.
        AnchorStateRegistry asrImpl = new AnchorStateRegistry(asrFinalityDelay);
        Proxy asrProxy = new Proxy(proxyAdmin);

        Proposal memory startingAnchor = Proposal({
            root: Hash.wrap(bytes32(0)),
            l2SequenceNumber: 0
        });
        // Set respected type to Compose Validity (5555) at init so games created now are respected.
        GameType composeType = GameType.wrap(5555);
        bytes memory asrInitData = abi.encodeWithSelector(
            AnchorStateRegistry.initialize.selector,
            ISystemConfig(systemConfig),
            dgf,
            startingAnchor,
            composeType
        );
        ProxyAdmin(proxyAdmin).upgradeAndCall(
            payable(address(asrProxy)),
            address(asrImpl),
            asrInitData
        );
        console.log("AnchorStateRegistry (proxy):", address(asrProxy));

        vm.stopBroadcast();

        return (address(asrProxy), address(gameImpl));
    }
}
