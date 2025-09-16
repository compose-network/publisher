// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";

import {ComposeL2OutputOracle} from "../src/ComposeL2OutputOracle.sol";
import {ComposeDisputeGame} from "../src/ComposeDisputeGame.sol";

import {DisputeGameFactory} from "@optimism/src/dispute/DisputeGameFactory.sol";

import {Proxy} from "@optimism/src/universal/Proxy.sol";
import {ProxyAdmin} from "@optimism/src/universal/ProxyAdmin.sol";

contract ComposeInfraDeployer is Script {
    struct L2OOEnv {
        address proposer;
        address owner;
        bytes32 aggregationVkey;
        uint256 startingSuperBlockNumber;
        address verifier;
    }

    function _readL2OOEnv() internal view returns (L2OOEnv memory e) {
        e.proposer = vm.envOr("COMPOSE_PROPOSER", address(0)); // pending from Karol
        e.owner = vm.envOr("ADMIN_ADDR", address(0));
        e.aggregationVkey = vm.envOr("COMPOSE_AGG_VKEY", bytes32(0));
        e.startingSuperBlockNumber = vm.envOr(
            "COMPOSE_STARTING_SUPERBLOCK_NUMBER",
            uint256(0)
        );
        e.verifier = vm.envOr("SP1_VERIFIER", address(0));
    }

    function run() public returns (address dgfProxyAddr, address gameImplAddr) {
        uint256 deployPk = vm.envOr("DEPLOYER_PRIVATE_KEY", uint256(0));
        if (deployPk != uint256(0)) {
            vm.startBroadcast(deployPk);
        } else {
            vm.startBroadcast();
        }

        address admin = vm.envOr("ADMIN_ADDR", msg.sender);
        // address systemConfig = vm.envAddress("SYSTEM_CONFIG_ADDR"); // TODO: used by optimism, change it
        // uint256 asrFinalityDelay = vm.envOr(
        //     "ASR_FINALITY_DELAY_SECONDS",
        //     uint256(1 days)
        // );

        // 1) Deploy ProxyAdmin controlled by `admin`.
        ProxyAdmin proxyAdmin = new ProxyAdmin(admin);
        console.log("ProxyAdmin:", address(proxyAdmin));

        // 2) Deploy DisputeGameFactory behind Bedrock Proxy and initialize via ProxyAdmin (satisfies
        //    ProxyAdminOwnedBase checks in initialize()).
        DisputeGameFactory dgfImpl = new DisputeGameFactory();
        Proxy dgfProxy = new Proxy(address(proxyAdmin));
        bytes memory dgfInitData = abi.encodeWithSelector(
            DisputeGameFactory.initialize.selector,
            admin
        );
        proxyAdmin.upgradeAndCall(
            payable(address(dgfProxy)),
            address(dgfImpl),
            dgfInitData
        );
        DisputeGameFactory dgf = DisputeGameFactory(address(dgfProxy));
        console.log("DisputeGameFactory (proxy):", address(dgf));

        // 3) Deploy ComposeL2OutputOracle behind Bedrock Proxy using ProxyAdmin.
        ComposeL2OutputOracle l2ooImpl = new ComposeL2OutputOracle();
        Proxy l2ooProxy = new Proxy(address(proxyAdmin));

        L2OOEnv memory cfg = _readL2OOEnv();
        // Default proposer/owner to `admin` if not provided.
        if (cfg.proposer == address(0)) cfg.proposer = admin;
        if (cfg.owner == address(0)) cfg.owner = admin;
        require(
            cfg.verifier != address(0),
            "ComposeInfra: SP1 verifier not set (SP1_VERIFIER)"
        );

        ComposeL2OutputOracle.InitParams
            memory initParams = ComposeL2OutputOracle.InitParams({
                proposer: cfg.proposer,
                owner: cfg.owner,
                aggregationVkey: cfg.aggregationVkey,
                startingSuperBlockNumber: cfg.startingSuperBlockNumber,
                verifier: cfg.verifier
            });

        bytes memory l2ooInitData = abi.encodeWithSelector(
            ComposeL2OutputOracle.initialize.selector,
            initParams
        );
        proxyAdmin.upgradeAndCall(
            payable(address(l2ooProxy)),
            address(l2ooImpl),
            l2ooInitData
        );
        console.log("ComposeL2OutputOracle (proxy):", address(l2ooProxy));

        vm.stopBroadcast();

        return (address(l2ooProxy), address(dgf));
    }
}
