// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Setup } from "@ssv/test/Setup.t.sol";
import { Mailbox } from "@ssv/src/Mailbox.sol";
import { PingPong } from "@ssv/src/PingPong.sol";

contract MailboxTest is Setup {
    function testShouldRevertNonCoordinatorToWriteToInbox() public {
        vm.prank(DEPLOYER);
        vm.expectRevert(
            abi.encodeWithSelector(Mailbox.InvalidCoordinator.selector)
        );
        mailbox.putInbox(1, address(0x01), address(0x01), 1, "SWAP", "hello");
    }

    function testWriteOutboxSingle() public returns (bytes32 key) {
        vm.prank(DEPLOYER);
        mailbox.write(2, COORDINATOR, 1, "SWAP", "hello");
        key = mailbox.getKey(1, 2, DEPLOYER, COORDINATOR, 1, "SWAP");
        assertEq(mailbox.outbox(key), "hello", "The message should match");
    }

    function testWriteInboxSingle() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(2, DEPLOYER, DEPLOYER, 1, "SWAP", "salut");
        key = mailbox.getKey(2, 1, DEPLOYER, DEPLOYER, 1, "SWAP");
        assertEq(mailbox.inbox(key), "salut", "The message should match");
    }

    function testWriteOutboxMultiple() public returns (bytes32 key) {
        testWriteOutboxSingle();
        vm.prank(DEPLOYER);
        mailbox.write(2, COORDINATOR, 1, "SWAP", "hello2");
        key = mailbox.getKey(1, 2, DEPLOYER, COORDINATOR, 1, "SWAP");
        assertEq(mailbox.outbox(key), "hello2", "The message should match");
    }

    function testWriteInboxMultiple() public returns (bytes32 key) {
        testWriteInboxSingle();
        vm.prank(COORDINATOR);
        mailbox.putInbox(2, DEPLOYER, DEPLOYER, 1, "SWAP", "salut2");
        key = mailbox.getKey(2, 1, DEPLOYER, DEPLOYER, 1, "SWAP");
        assertEq(mailbox.inbox(key), "salut2", "The message should match");
    }

    function testRead() public {
        testWriteInboxSingle();
        bytes memory data = mailbox.read(2, DEPLOYER, DEPLOYER, 1, "SWAP");
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
}
