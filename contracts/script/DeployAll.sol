// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script, console } from "forge-std/Script.sol";
import { stdJson } from "forge-std/StdJson.sol";

import { Mailbox } from "@ssv/src/Mailbox.sol";
import { PingPong } from "@ssv/src/PingPong.sol";
import { BridgeableToken } from "@ssv/src/BridgeableToken.sol";
import { Bridge } from "@ssv/src/Bridge.sol";

contract DeployAll is Script {
    using stdJson for string;

    function _deployAll() internal returns (string memory) {
        bytes32 salt = vm.envBytes32("DEPLOY_SALT");

        vm.startBroadcast();

        address coordinator = vm.envAddress("DEPLOYER_ADDRESS");

        // mailbox address
        address mailboxAddr = _deployCreate2(
            salt,
            abi.encodePacked(
                type(Mailbox).creationCode,
                abi.encode(coordinator)
            )
        );
        Mailbox mailbox = Mailbox(mailboxAddr);

        // pingpong address
        address pingPongAddr = _deployCreate2(
            salt,
            abi.encodePacked(
                type(PingPong).creationCode,
                abi.encode(address(mailbox))
            )
        );
        PingPong pingPong = PingPong(pingPongAddr);

        // bridge address
        address bridgeAddr = _deployCreate2(
            salt,
            abi.encodePacked(
                type(Bridge).creationCode,
                abi.encode(address(mailbox))
            )
        );
        Bridge bridge = Bridge(bridgeAddr);

        // token address
        address tokenAddr = _deployCreate2(
            salt,
            type(BridgeableToken).creationCode // no constructor args
        );
        BridgeableToken token = BridgeableToken(tokenAddr);

        vm.stopBroadcast();

        console.log("Mailbox:  ", address(mailbox));
        console.log("PingPong:  ", address(pingPong));
        console.log("Coordinator:  ", coordinator);
        console.log("Token:  ", address(token));
        console.log("Bridge:  ", address(bridge));

        return saveToJson(coordinator, bridge, pingPong, mailbox, token);
    }

    function saveToJson(
        address coordinator,
        Bridge bridge,
        PingPong pingPong,
        Mailbox mailbox,
        BridgeableToken token
    ) internal returns (string memory) {
        string memory parent = "parent";

        string memory deployed_addresses = "addresses";
        vm.serializeAddress(deployed_addresses, "Mailbox", address(mailbox));
        vm.serializeAddress(deployed_addresses, "PingPong", address(pingPong));
        vm.serializeAddress(deployed_addresses, "Token", address(token));
        vm.serializeAddress(deployed_addresses, "Bridge", address(bridge));

        string memory deployed_addresses_output = vm.serializeAddress(
            deployed_addresses,
            "Coordinator",
            coordinator
        );

        string memory chain_info = "chainInfo";
        vm.serializeUint(chain_info, "deploymentBlock", block.number);
        string memory chain_info_output = vm.serializeUint(
            chain_info,
            "chainId",
            block.chainid
        );

        vm.serializeString(
            parent,
            deployed_addresses,
            deployed_addresses_output
        );
        return vm.serializeString(parent, chain_info, chain_info_output);
    }

    function _deployCreate2(bytes32 salt, bytes memory code) internal returns (address addr) {
        assembly {
            addr := create2(0, add(code, 0x20), mload(code), salt)
            if iszero(extcodesize(addr)) { revert(0, 0) }
        }
    }
}