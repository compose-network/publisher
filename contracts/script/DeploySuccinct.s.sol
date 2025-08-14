// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";

import {Proposer} from "../src/OPSuccinct-draft/Proposer.sol";
import {L2OutputOracle} from "../src/OPSuccinct-draft/L2OutputOracle.sol";
import {BatchInbox} from "../src/OPSuccinct-draft/BatchInbox.sol";

contract DeploySuccinct is Script {
    function run() external {
        vm.startBroadcast();

        BatchInbox inbox = new BatchInbox();
        console.log("BatchInbox deployed at:", address(inbox));

        L2OutputOracle oracle = new L2OutputOracle();
        console.log("L2OutputOracle deployed at:", address(oracle));

        Proposer proposer = new Proposer(oracle, inbox);
        console.log("Proposer deployed at:", address(proposer));

        vm.stopBroadcast();
    }
}
