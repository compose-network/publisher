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

        address coordinator = vm.envOr("DEPLOYER_ADDRESS", address(this));

        Mailbox mailboxC = new Mailbox(coordinator);

        //mailbox a = 0x33C061304de440B89BC829bD4dC4eF688E5d1Cef
        // mailbox b = 0xbB6A1eCF93641122E5c76b6978bb4B7304879Dd5
        // is it the same for rollupA and rollupB?
        address mailbox = 0x33C061304de440B89BC829bD4dC4eF688E5d1Cef;
        // address mailbox = address(mailboxC);
        // PingPong pingPong = new PingPong(mailbox);
        Bridge bridge = new Bridge(mailbox);
        // MyToken myToken = new MyToken(); // MyToken = 0x6d19CB7639DeB366c334BD69f030A38e226BA6d2

        vm.stopBroadcast();

        console.log("Mailbox:  ", address(mailbox));
        // console.log("PingPong:  ", address(pingPong));
        console.log("Coordinator:  ", coordinator);
        // console.log("MyToken:  ", address(myToken));
        console.log("Bridge:  ", address(bridge));

        // return saveToJson(mailbox, pingPong, coordinator, myToken, bridge);
        return saveToJson(coordinator, bridge);
    }

    function saveToJson(
        address coordinator,
        Bridge bridge
    ) internal returns (string memory) {
        string memory parent = "parent";

        string memory deployed_addresses = "addresses";
        // vm.serializeAddress(deployed_addresses, "Mailbox", address(mailbox));
        // vm.serializeAddress(deployed_addresses, "PingPong", address(pingPong));
        // vm.serializeAddress(deployed_addresses, "MyToken", address(myToken));
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
