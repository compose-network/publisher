// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Setup } from "@ssv/test/Setup.t.sol";

contract BridgeTest is Setup {
    function testSend() public {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, COORDINATOR, 1, "test message", "ACK SEND");
        bytes32 key = mailbox.getKey(1, 2, COORDINATOR, 1, "ACK SEND");
        assertEq(
            mailbox.inbox(key),
            "test message",
            "The message should match"
        );
        vm.startPrank(DEPLOYER);
        myToken.mint(DEPLOYER, 100);
        bridge.send(1, 2, address(myToken), DEPLOYER, COORDINATOR, 100, 1);
    }
    function testReceive() public {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, COORDINATOR, 1, "test message", "SEND");
        bytes32 key = mailbox.getKey(1, 2, COORDINATOR, 1, "SEND");
        assertEq(
            mailbox.inbox(key),
            "test message",
            "The message should match"
        );
        vm.startPrank(DEPLOYER);
        // bridge.receive(1, 2, DEPLOYER, COORDINATOR, 1);
        // bridge.receive(chainSrc, chainDest, sender, receiver, sessionId);(1, 2, DEPLOYER, COORDINATOR, 1);
        // bridge.receive(chainSrc, chainDest, sender, receiver, sessionId);
    }
}
