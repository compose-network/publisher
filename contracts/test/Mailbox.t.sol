// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.30;

import { Test } from "forge-std/Test.sol";
import { Mailbox } from "@ssv/src/Mailbox.sol";
import { PingPong } from "@ssv/src/PingPong.sol";
import { MyToken } from "@ssv/src/Token.sol";

contract MailboxTest is Test {
    Mailbox public mailbox;
    PingPong public pingPong;
    MyToken public myToken;

    address public immutable DEPLOYER = makeAddr("Deployer");
    address public immutable COORDINATOR = makeAddr("Coordinator");

    uint256 public constant INITIAL_ETH_BALANCE = 10 ether;

    function setUp() public {
        vm.label(DEPLOYER, "Deployer");
        vm.label(COORDINATOR, "Coordinator");

        vm.deal(DEPLOYER, INITIAL_ETH_BALANCE);
        vm.deal(COORDINATOR, INITIAL_ETH_BALANCE);

        vm.prank(DEPLOYER);
        mailbox = new Mailbox(address(COORDINATOR));
        pingPong = new PingPong(address(mailbox));

        vm.label(address(mailbox), "Mailbox");
        vm.label(address(pingPong), "PingPong");
    }

    function testShouldRevertNonCoordinatorToWriteToInbox() public {
        vm.prank(DEPLOYER);
        vm.expectRevert(
            abi.encodeWithSelector(Mailbox.InvalidCoordinator.selector)
        );
        mailbox.putInbox(1, 2, address(0x01), 1, "hello", "SWAP");
    }

    function testWriteOutboxSingle() public returns (bytes32 key) {
        vm.prank(DEPLOYER);
        mailbox.write(1, 2, COORDINATOR, 1, "hello", "SWAP");
        key = mailbox.getKey(1, 2, DEPLOYER, COORDINATOR, 1, "SWAP");
        assertEq(mailbox.outbox(key), "hello", "The message should match");
    }

    function testWriteInboxSingle() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, DEPLOYER, 1, "salut", "SWAP");
        key = mailbox.getKey(1, 2, COORDINATOR, DEPLOYER, 1, "SWAP");
        assertEq(mailbox.inbox(key), "salut", "The message should match");
    }

    function testWriteOutboxMultiple() public returns (bytes32 key) {
        testWriteOutboxSingle();
        vm.prank(DEPLOYER);
        mailbox.write(1, 2, COORDINATOR, 1, "hello2", "SWAP");
        key = mailbox.getKey(1, 2, DEPLOYER, COORDINATOR, 1, "SWAP");
        assertEq(mailbox.outbox(key), "hello2", "The message should match");
    }

    function testWriteInboxMultiple() public returns (bytes32 key) {
        testWriteInboxSingle();
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, DEPLOYER, 1, "salut2", "SWAP");
        key = mailbox.getKey(1, 2, COORDINATOR, DEPLOYER, 1, "SWAP");
        assertEq(mailbox.inbox(key), "salut2", "The message should match");
    }

    function testRead() public {
        testWriteInboxSingle();
        bytes memory data = mailbox.read(
            1,
            2,
            COORDINATOR,
            DEPLOYER,
            1,
            "SWAP"
        );
        assertEq(data, "salut", "Should match the read message");
    }

    function testClearOutbox() public {
        bytes32 key = testWriteOutboxSingle();
        vm.prank(COORDINATOR);
        mailbox.clear();
        assertEq(mailbox.outbox(key), "", "The outbox data should be empty");
    }

    function testClearInbox() public {
        bytes32 key = testWriteInboxSingle();
        vm.prank(COORDINATOR);
        mailbox.clear();
        assertEq(mailbox.inbox(key), "", "The inbox data should be empty");
    }

    function testWritePingToInbox() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, DEPLOYER, 1, "first ping", "PING");
        key = mailbox.getKey(1, 2, COORDINATOR, DEPLOYER, 1, "PING");
        assertEq(mailbox.inbox(key), "first ping", "The message should match");
    }
    function testWritePongToInbox() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, DEPLOYER, 1, "first pong", "PONG");
        key = mailbox.getKey(1, 2, COORDINATOR, DEPLOYER, 1, "PONG");
        assertEq(mailbox.inbox(key), "first pong", "The message should match");
    }

    function testPing() public {
        vm.prank(address(DEPLOYER));
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
            1,
            2,
            COORDINATOR,
            DEPLOYER,
            1,
            "first pong"
        );
        assertEq(ping, "first ping", "Should return the right ping");
    }

    function testPingAfterPong() public {
        testWritePongToInbox();
        bytes memory pong = pingPong.ping(
            1,
            2,
            COORDINATOR,
            DEPLOYER,
            1,
            "first ping"
        );
        assertEq(pong, "first pong", "Should return the right pong");
    }
}
