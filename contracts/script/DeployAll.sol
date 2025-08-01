// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script, console } from "forge-std/Script.sol";
import { stdJson } from "forge-std/StdJson.sol";

import { Mailbox } from "@ssv/src/Mailbox.sol";
import { PingPong } from "@ssv/src/PingPong.sol";
import { MyToken } from "@ssv/src/Token.sol";

contract DeployAll is Script {
    using stdJson for string;

    function _deployAll() internal returns (string memory) {
        vm.startBroadcast();

        address coordinator = vm.envOr("DEPLOYER_ADDRESS", address(this));

        Mailbox mailbox = new Mailbox(coordinator);
        PingPong pingPong = new PingPong(address(mailbox));
        MyToken myToken = new MyToken();

        vm.stopBroadcast();

        console.log("Mailbox:  ", address(mailbox));
        console.log("PingPong:  ", address(pingPong));
        console.log("Coordinator:  ", coordinator);
        console.log("MyToken:  ", address(myToken));

        return saveToJson(mailbox, pingPong, coordinator, myToken);
    }

    function saveToJson(
        Mailbox mailbox,
        PingPong pingPong,
        address coordinator,
        MyToken myToken
    ) internal returns (string memory) {
        string memory parent = "parent";

        string memory deployed_addresses = "addresses";
        vm.serializeAddress(deployed_addresses, "Mailbox", address(mailbox));
        vm.serializeAddress(deployed_addresses, "PingPong", address(pingPong));
        vm.serializeAddress(deployed_addresses, "MyToken", address(myToken));

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
