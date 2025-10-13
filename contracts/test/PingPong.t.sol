// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Setup } from "@ssv/test/Setup.t.sol";
import { PingPong } from "@ssv/src/PingPong.sol";
import { IPingPong } from "@ssv/src/interfaces/IPingPong.sol";

contract PingPongTest is Setup {

    uint256 internal thisChain = block.chainid;
    uint256 internal otherChain = 2;

    function testWritePingToInbox() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(otherChain, DEPLOYER, address(pingPong), 1, "PING", "first ping");
        key = mailbox.getKey(otherChain, thisChain, DEPLOYER, address(pingPong), 1, "PING");
        assertEq(mailbox.inbox(key), "first ping", "The message should match");
    }
    function testWritePongToInbox() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(otherChain, DEPLOYER, address(pingPong), 1, "PONG", "first pong");
        key = mailbox.getKey(otherChain, thisChain, DEPLOYER, address(pingPong), 1, "PONG");
        assertEq(mailbox.inbox(key), "first pong", "The message should match");
    }

    function testPing() public {
        vm.prank(COORDINATOR);
        // ping message from pingPong on dest chain to pingPong on this chain
        mailbox.putInbox(otherChain, DEPLOYER, address(pingPong), 1, "PONG", "");
        vm.prank(DEPLOYER);
        vm.expectRevert(
            abi.encodeWithSelector(IPingPong.PongMessageEmpty.selector)
        );
        bytes memory pong = pingPong.ping(
            otherChain,
            DEPLOYER,
            DEPLOYER,
            1,
            "ping outbox1"
        );
        assertEq(pong, "", "Should return empty pong");
    }

    function testPong() public {
        vm.prank(COORDINATOR);
        // message from pingPong on other chain to pingPong on this chain
        mailbox.putInbox(otherChain, DEPLOYER, address(pingPong), 1, "PING", "");
        vm.prank(DEPLOYER);
        vm.expectRevert(
            abi.encodeWithSelector(IPingPong.PingMessageEmpty.selector)
        );
        bytes memory ping = pingPong.pong(
            otherChain,
            DEPLOYER,
            1,
            "pong outbox1"
        );
        assertEq(ping, "", "Should return empty ping");
    }

    function testPongAfterPing() public {
        testWritePingToInbox();
        bytes memory ping = pingPong.pong(
            otherChain,
            DEPLOYER,
            1,
            "first pong"
        );
        assertEq(ping, "first ping", "Should return the right ping");
    }

    function testPingAfterPong() public {
        testWritePongToInbox();
        bytes memory pong = pingPong.ping(
            otherChain,
            DEPLOYER,
            DEPLOYER,
            1,
            "first ping"
        );
        assertEq(pong, "first pong", "Should return the right pong");
    }
}
