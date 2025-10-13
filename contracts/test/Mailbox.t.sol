// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";
import { Setup } from "@ssv/test/Setup.t.sol";

contract MailboxTest is Setup {
    uint256 internal thisChain = block.chainid;
    uint256 internal otherChain = 2;

    address internal messageSender = address(0xabc);
    address internal messageReceiver = address(0x123);

    /// @dev Tests constructor sets values correctly
    function testConstructor() public {
        assertEq(
            mailbox.COORDINATOR(),
            COORDINATOR,
            "Coordinator should be set"
        );
    }

    /// @dev Tests that non-coordinator cannot write to inbox
    function testShouldRevertNonCoordinatorToWriteToInbox() public {
        vm.prank(messageSender);
        vm.expectRevert(IMailbox.InvalidCoordinator.selector);
        mailbox.putInbox(otherChain, messageSender, messageReceiver, 1, "SWAP", "hello");
    }

    /// @dev Tests that non-coordinator cannot clear the mailbox
//    function testShouldRevertNonCoordinatorClear() public {
//        vm.prank(DEPLOYER);
//        vm.expectRevert(IMailbox.InvalidCoordinator.selector);
//        mailbox.clear();
//    }

    /// @dev Tests writing a single message to outbox
    function testWriteOutboxSingle() public returns (bytes32 key) {
        vm.startPrank(messageSender);

        vm.expectEmit(true, true, false, true);
        emit IMailbox.NewOutboxKey(
            0,
            mailbox.getKey(thisChain, otherChain, messageSender, messageReceiver, 1, "SWAP")
        );
        mailbox.write(otherChain, messageReceiver, 1, "SWAP", "hello");
        vm.stopPrank();

        key = mailbox.getKey(thisChain, otherChain, messageSender, messageReceiver, 1, "SWAP");
        assertEq(mailbox.outbox(key), "hello", "The message should match");
        assertTrue(mailbox.createdKeys(key), "Key should be created");

        // check header
        (
            uint256 hChainSrc,
            uint256 hChainDest,
            address hSender,
            address hReceiver,
            uint256 hSessionId,
            bytes memory hLabel
        ) = mailbox.messageHeaderListOutbox(0);
        assertEq(hChainSrc, thisChain, "Source chain should match");
        assertEq(hChainDest, otherChain, "Dest chain should match");
        assertEq(hSender, messageSender, "Sender should match");
        assertEq(hReceiver, messageReceiver, "Receiver should match");
        assertEq(hSessionId, 1, "Session ID should match");
        assertEq(keccak256(hLabel), keccak256("SWAP"), "Label should match");

        bytes32 expectedRoot = keccak256(abi.encode(0, key, "hello"));
        assertEq(
            mailbox.outboxRootPerChain(otherChain),
            expectedRoot,
            "Outbox root should match"
        );
    }

    /// @dev Tests writing a single message to inbox by coordinator
    function testWriteInboxSingle() public returns (bytes32 key) {
        vm.startPrank(COORDINATOR);

        vm.expectEmit(true, true, false, true);
        emit IMailbox.NewInboxKey(
            0,
            mailbox.getKey(otherChain, thisChain, messageSender, messageReceiver, 1, "SWAP")
        );

        mailbox.putInbox(otherChain, messageSender, messageReceiver, 1, "SWAP", "salut");
        vm.stopPrank();

        key = mailbox.getKey(otherChain, thisChain, messageSender, messageReceiver, 1, "SWAP");
        assertEq(mailbox.inbox(key), "salut", "The message should match");
        assertTrue(mailbox.createdKeys(key), "Key should be created");

        (
            uint256 hChainSrc,
            uint256 hChainDest,
            address hSender,
            address hReceiver,
            uint256 hSessionId,
            bytes memory hLabel
        ) = mailbox.messageHeaderListInbox(0);
        assertEq(hChainSrc, otherChain, "Source chain should match");
        assertEq(hChainDest, thisChain, "Dest chain should match");
        assertEq(hSender, messageSender, "Sender should match");
        assertEq(hReceiver, messageReceiver, "Receiver should match");
        assertEq(hSessionId, 1, "Session ID should match");
        assertEq(keccak256(hLabel), keccak256("SWAP"), "Label should match");

        bytes32 expectedRoot = keccak256(abi.encode(0, key, "salut"));
        assertEq(mailbox.inboxRootPerChain(otherChain), expectedRoot, "Inbox root should match");
    }

    /// @dev Tests writing multiple messages to outbox
    function testWriteOutboxMultiple() public {
        bytes32 key1 = testWriteOutboxSingle();

        vm.startPrank(messageSender);

        vm.expectEmit(true, true, false, true);
        emit IMailbox.NewOutboxKey(
            1,
            mailbox.getKey(thisChain, otherChain, messageSender, messageReceiver, 2, "SWAP")
        );

        mailbox.write(otherChain, messageReceiver, 2, "SWAP", "hello2");
        vm.stopPrank();

        bytes32 key2 = mailbox.getKey(
            thisChain,
            otherChain,
            messageSender,
            messageReceiver,
            2,
            "SWAP"
        );
        assertEq(mailbox.outbox(key1), "hello", "First message should remain");
        assertEq(mailbox.outbox(key2), "hello2", "Second message should match");

        bytes32 root1 = keccak256(abi.encode(0, key1, "hello"));
        bytes32 expectedRoot2 = keccak256(abi.encode(root1, key2, "hello2"));
        assertEq(
            mailbox.outboxRootPerChain(otherChain),
            expectedRoot2,
            "Outbox root should be chained"
        );
    }

    /// @dev Tests writing multiple messages to inbox
    function testWriteInboxMultiple() public {
        bytes32 key1 = testWriteInboxSingle();

        vm.startPrank(COORDINATOR);

        vm.expectEmit(true, true, false, true);
        emit IMailbox.NewInboxKey(
            1,
            mailbox.getKey(otherChain, thisChain, messageSender, messageReceiver, 2, "SWAP")
        );

        mailbox.putInbox(otherChain, messageSender, messageReceiver, 2, "SWAP", "salut2");
        vm.stopPrank();

        bytes32 key2 = mailbox.getKey(
            otherChain,
            thisChain,
            messageSender,
            messageReceiver,
            2,
            "SWAP"
        );
        assertEq(mailbox.inbox(key1), "salut", "First message should remain");
        assertEq(mailbox.inbox(key2), "salut2", "Second message should match");

        bytes32 root1 = keccak256(abi.encode(0, key1, "salut"));
        bytes32 expectedRoot2 = keccak256(abi.encode(root1, key2, "salut2"));
        assertEq(
            mailbox.inboxRootPerChain(otherChain),
            expectedRoot2,
            "Inbox root should be chained"
        );
    }

    /// @dev Tests reading a message from inbox
    function testRead() public {
        testWriteInboxSingle();
        vm.prank(messageReceiver);
        bytes memory data = mailbox.read(
            otherChain,
            messageSender,
            1,
            "SWAP"
        );
        assertEq(data, "salut", "Should match the read message");
    }

    /// @dev Tests reading an empty but created message returns empty sata
    function testReadEmptyCreated() public {
        vm.prank(COORDINATOR);
        mailbox.putInbox(otherChain, messageSender, messageReceiver, 1, "SWAP", "");
        vm.prank(messageReceiver);
        bytes memory data = mailbox.read(
            otherChain,
            messageSender,
            1,
            "SWAP"
        );
        assertEq(data, "", "Should return empty message");
    }

    /// @dev Tests reading non-existent message reverts
    function testReadNotFound() public {
        vm.expectRevert(IMailbox.MessageNotFound.selector);
        mailbox.read(otherChain, messageSender, 1, "SWAP");
    }

    /// @dev Tests clearing outbox
//    function testClearOutbox() public {
//        bytes32 key = testWriteOutboxSingle();
//        vm.prank(COORDINATOR);
//        mailbox.clear();
//        assertEq(mailbox.outbox(key), "", "The outbox data should be empty");
//        assertFalse(mailbox.createdKeys(key), "Created key should be deleted");
//        assertEq(mailbox.outboxRoot(), 0, "Outbox root should be reset");
//    }

    /// @dev Tests clearing inbox
//    function testClearInbox() public {
//        bytes32 key = testWriteInboxSingle();
//        vm.prank(COORDINATOR);
//        mailbox.clear();
//        assertEq(mailbox.inbox(key), "", "The inbox data should be empty");
//        assertFalse(mailbox.createdKeys(key), "Created key should be deleted");
//        assertEq(mailbox.inboxRoot(), 0, "Inbox root should be reset");
//    }

    /// @dev Tests computeKey function
    function testComputeKey() public {
        testWriteInboxSingle();
        bytes32 computed = mailbox.computeKey(0);
        bytes32 expected = mailbox.getKey(
            otherChain,
            thisChain,
            messageSender,
            messageReceiver,
            1,
            "SWAP"
        );
        assertEq(computed, expected, "Computed key should match");
    }

    /// @dev Tests computeKey reverts on invalid id
    function testComputeKeyInvalidId() public {
        vm.expectRevert(IMailbox.InvalidId.selector);
        mailbox.computeKey(0);
    }

    /// @dev Tests getKey is consistent
    function testGetKeyPure() public view {
        bytes32 key1 = mailbox.getKey(
            1,
            2,
            address(0xA),
            address(0xB),
            1,
            "LABEL"
        );
        bytes32 key2 = mailbox.getKey(
            1,
            2,
            address(0xA),
            address(0xB),
            1,
            "LABEL"
        );
        assertEq(key1, key2, "Keys should be the same");
        bytes32 keyDiff = mailbox.getKey(
            1,
            2,
            address(0xA),
            address(0xB),
            1,
            "DIFF"
        );
        assertNotEq(
            key1,
            keyDiff,
            "Different labels should give different keys"
        );
    }
}
