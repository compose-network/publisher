// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.30;

import { Script, console } from "forge-std/Script.sol";
import { stdJson } from "forge-std/StdJson.sol";

import { Mailbox } from "@ssv/src/Mailbox.sol";
import { PingPong } from "@ssv/src/PingPong.sol";

contract DeployAll is Script {
    using stdJson for string;

    function _deployAll(string memory json) internal returns (string memory) {
        vm.startBroadcast();

        address coordinator = vm.envOr("DEPLOYER_ADDRESS", address(this));

        Mailbox mailbox = new Mailbox(coordinator);
        PingPong pingpong = new PingPong(address(mailbox));

        vm.stopBroadcast();

        console.log("Mailbox:  ", address(mailbox));
        console.log("PingPong:  ", address(pingpong));
        console.log("Coordinator:  ", coordinator);

        return saveToJson(mailbox, pingpong, coordinator, json);
    }

    function saveToJson(
        Mailbox mailbox,
        PingPong pingpong,
        address coordinator,
        string memory json
    ) internal returns (string memory) {
        string memory parent = "parent";

        string memory deployed_addresses = "addresses";
        vm.serializeAddress(deployed_addresses, "Mailbox", address(mailbox));
        vm.serializeAddress(deployed_addresses, "PingPong", address(pingpong));

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

        // serialize all the data
        vm.serializeString(
            parent,
            deployed_addresses,
            deployed_addresses_output
        );
        return vm.serializeString(parent, chain_info, chain_info_output);
    }
}
