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
        mailbox.putInbox(1, 2, address(0x01), 1, "hello", "SWAP");
    }

    function testWriteOutboxSingle() public returns (bytes32 key) {
        vm.prank(DEPLOYER);
        mailbox.write(1, 2, COORDINATOR, 1, "hello", "SWAP");
        key = mailbox.getKey(1, 2, COORDINATOR, 1, "SWAP");
        assertEq(mailbox.outbox(key), "hello", "The message should match");
    }

    function testWriteInboxSingle() public returns (bytes32 key) {
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, DEPLOYER, 1, "salut", "SWAP");
        key = mailbox.getKey(1, 2, DEPLOYER, 1, "SWAP");
        assertEq(mailbox.inbox(key), "salut", "The message should match");
    }

    function testWriteOutboxMultiple() public returns (bytes32 key) {
        testWriteOutboxSingle();
        vm.prank(DEPLOYER);
        mailbox.write(1, 2, COORDINATOR, 1, "hello2", "SWAP");
        key = mailbox.getKey(1, 2, COORDINATOR, 1, "SWAP");
        assertEq(mailbox.outbox(key), "hello2", "The message should match");
    }

    function testWriteInboxMultiple() public returns (bytes32 key) {
        testWriteInboxSingle();
        vm.prank(COORDINATOR);
        mailbox.putInbox(1, 2, DEPLOYER, 1, "salut2", "SWAP");
        key = mailbox.getKey(1, 2, DEPLOYER, 1, "SWAP");
        assertEq(mailbox.inbox(key), "salut2", "The message should match");
    }

    function testRead() public {
        testWriteInboxSingle();
        bytes memory data = mailbox.read(1, 2, DEPLOYER, 1, "SWAP");
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
