// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Setup } from "@ssv/test/Setup.t.sol";
import { PingPong } from "@ssv/src/PingPong.sol";

contract PingPongTest is Setup {
    function testWritePingToInbox() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(2, DEPLOYER, DEPLOYER, 1, "PING", "first ping");
        key = mailbox.getKey(2, 1, DEPLOYER, DEPLOYER, 1, "PING");
        assertEq(mailbox.inbox(key), "first ping", "The message should match");
    }
    function testWritePongToInbox() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(2, DEPLOYER, DEPLOYER, 1, "PONG", "first pong");
        key = mailbox.getKey(2, 1, DEPLOYER, DEPLOYER, 1, "PONG");
        assertEq(mailbox.inbox(key), "first pong", "The message should match");
    }

    function testPing() public {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, DEPLOYER, DEPLOYER, 1, "PONG", "");
        vm.prank(DEPLOYER);
        vm.expectRevert(
            abi.encodeWithSelector(PingPong.PongMessageEmpty.selector)
        );
        bytes memory pong = pingPong.ping(
            1,
            2,
            DEPLOYER,
            DEPLOYER,
            1,
            "ping outbox1"
        );
        assertEq(pong, "", "Should return empty pong");
    }

    function testPong() public {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, DEPLOYER, DEPLOYER, 1, "PING", "");
        vm.prank(DEPLOYER);
        vm.expectRevert(
            abi.encodeWithSelector(PingPong.PingMessageEmpty.selector)
        );
        bytes memory ping = pingPong.pong(
            1,
            2,
            DEPLOYER,
            DEPLOYER,
            1,
            "pong outbox1"
        );
        assertEq(ping, "", "Should return empty ping");
    }

    function testPongAfterPing() public {
        testWritePingToInbox();
        bytes memory ping = pingPong.pong(
            2,
            1,
            DEPLOYER,
            DEPLOYER,
            1,
            "first pong"
        );
        assertEq(ping, "first ping", "Should return the right ping");
    }

    function testPingAfterPong() public {
        testWritePongToInbox();
        bytes memory pong = pingPong.ping(
            2,
            1,
            DEPLOYER,
            DEPLOYER,
            1,
            "first ping"
        );
        assertEq(pong, "first pong", "Should return the right pong");
    }
}
