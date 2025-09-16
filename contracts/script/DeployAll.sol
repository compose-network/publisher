// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script, console } from "forge-std/Script.sol";
import { stdJson } from "forge-std/StdJson.sol";

import { Mailbox } from "@ssv/src/Mailbox.sol";
import { PingPong } from "@ssv/src/PingPong.sol";
import { MyToken } from "@ssv/src/Token.sol";
import { Bridge } from "@ssv/src/Bridge.sol";

contract DeployAll is Script {
    using stdJson for string;

    function _deployAll() internal returns (string memory) {
        vm.startBroadcast();

        address coordinator = vm.envAddress("DEPLOYER_ADDRESS");

        Mailbox mailbox = new Mailbox(coordinator, block.chainid);
        PingPong pingPong = new PingPong(address(mailbox));
        Bridge bridge = new Bridge(address(mailbox));
        MyToken myToken = new MyToken();

        vm.stopBroadcast();

        console.log("Mailbox:  ", address(mailbox));
        console.log("PingPong:  ", address(pingPong));
        console.log("Coordinator:  ", coordinator);
        console.log("MyToken:  ", address(myToken));
        console.log("Bridge:  ", address(bridge));

        return saveToJson(coordinator, bridge, pingPong, mailbox, myToken);
    }

    function saveToJson(
        address coordinator,
        Bridge bridge,
        PingPong pingPong,
        Mailbox mailbox,
        MyToken myToken
    ) internal returns (string memory) {
        string memory parent = "parent";

        string memory deployed_addresses = "addresses";
        vm.serializeAddress(deployed_addresses, "Mailbox", address(mailbox));
        vm.serializeAddress(deployed_addresses, "PingPong", address(pingPong));
        vm.serializeAddress(deployed_addresses, "MyToken", address(myToken));
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
}
