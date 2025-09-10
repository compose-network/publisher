// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Setup } from "@ssv/test/Setup.t.sol";

contract BridgeTest is Setup {
    uint256 internal constant chainA = 1;
    uint256 internal constant chainB = 2;

    /// @dev Tests sending tokens from chain A to chain B (burning tokens on chain A and putting a message to outbox)
    function testSend() public {
        address mockDestBridge = address(0xDEADBEEF);

        vm.startPrank(DEPLOYER);
        myToken.mint(DEPLOYER, 100);
        myToken.approve(address(bridge), 100);

        // send tokens to a bridge on chain B
        bridge.send(
            chainA, // source chain id
            chainB, // destination chain id
            address(myToken), // token address
            DEPLOYER, // sender of tokens
            COORDINATOR, // receiver of tokens on dest chain
            100, // amount of tokens
            1, // session ID
            mockDestBridge // dest chain bridge address
        );
        vm.stopPrank();

        // compute the expected key for the outbox message
        bytes32 key = mailbox.getKey(
            chainA, // source chain id
            chainB, // dest chain id
            address(bridge), // sender of the message is bridge contract
            COORDINATOR, // receiver address
            1, // session ID
            "SEND" // label
        );

        bytes memory expectedData = abi.encode(
            DEPLOYER,
            COORDINATOR,
            address(myToken),
            100
        );

        assertEq(
            mailbox.outbox(key),
            expectedData,
            "Outbox message should match"
        );
        assertEq(myToken.balanceOf(DEPLOYER), 0, "Tokens should be burned");
    }

    /// @dev Tests receiving tokens from a chain B on chain A (faking message from chain B, receiving tokens and sending OK status back)
    function testReceiveTokens() public {
        address mockSrcBridge = address(0xABCDEF);
        address sender = DEPLOYER; // original sender on source chain
        address receiver = COORDINATOR; //receiver on dest chain
        address token = address(myToken);
        uint256 amount = 100;
        bytes memory data = abi.encode(sender, receiver, token, amount);

        // put the message in inbox (from chain B)
        vm.prank(COORDINATOR);
        mailbox.putInbox(
            chainB, // source chain id
            mockSrcBridge, // sender address is source bridge
            receiver, // receiver address
            1, // session ID
            "SEND", // label
            data // data
        );

        vm.startPrank(receiver);

        // receive tokens on chain A
        (address receivedToken, uint256 receivedAmount) = bridge.receiveTokens(
            chainB, // source chain id (tokens incoming from chain B)
            chainA, // dest chain id (tokens were send to chain A)
            sender, // original sender of tokens
            receiver, // receiver address
            1, // session ID
            mockSrcBridge // source bridge address
        );
        vm.stopPrank();

        assertEq(receivedToken, token, "Received token should match");
        assertEq(receivedAmount, amount, "Received amount should match");

        assertEq(
            myToken.balanceOf(receiver),
            amount,
            "Tokens should be minted"
        );

        // compute ACK key in outbox to check OK response
        bytes32 ackKey = mailbox.getKey(
            chainA, // source chain (now chain A because reporting status back to chain B)
            chainB, // dest chain id (original source of tokens)
            address(bridge), // sender address (current bridge on chain A)
            sender, // receiver for ACK (original sender from chain B)
            1, // session id
            "ACK SEND" // label
        );

        assertEq(
            mailbox.outbox(ackKey),
            abi.encode("OK"),
            "ACK should be written"
        );
    }

    /// @dev Tests that bridge function to validate ACK messages works correctly
    function testCheckAck() public {
        address mockDestBridge = address(0xDEADBEEF);

        vm.prank(COORDINATOR);

        // put a mock ACK message in inbox from some dest chain
        mailbox.putInbox(
            chainB, // source (dest chain for ACK)
            mockDestBridge, // sender (dest bridge)
            DEPLOYER, // receiver (original sender)
            1, // session id
            "ACK SEND", // label
            abi.encode("OK") // data
        );

        bytes memory ack = bridge.checkAck(
            chainA, // original source chain id
            chainB, // original dest chain id
            DEPLOYER, // original sender
            COORDINATOR, // original receiver
            1, // session ID
            mockDestBridge // dest bridge
        );

        assertEq(ack, abi.encode("OK"), "ACK should match");
    }

    /// @dev Tests that only own address can be used as a sender
    function testSendWrongSender() public {
        address mockDestBridge = address(0xDEADBEEF);

        vm.startPrank(address(0xBAD));

        vm.expectRevert("Should be the real sender");
        bridge.send(
            chainA,
            chainB,
            address(myToken),
            DEPLOYER,
            COORDINATOR,
            100,
            1,
            mockDestBridge
        );
        vm.stopPrank();
    }

    /// @dev Tests that only receiver can claim tokens
    function testReceiveTokensWrongCaller() public {
        address mockSrcBridge = address(0xABCDEF);

        vm.startPrank(address(0xBAD));

        vm.expectRevert("Only receiver can claim");
        bridge.receiveTokens(
            chainB,
            chainA,
            DEPLOYER,
            COORDINATOR,
            1,
            mockSrcBridge
        );
        vm.stopPrank();
    }

    /// @dev Tests that receive is only possible if the message was not added before
    function testReceiveTokensNoMessage() public {
        address mockSrcBridge = address(0xABCDEF);

        vm.startPrank(COORDINATOR);

        vm.expectRevert();
        bridge.receiveTokens(
            chainB,
            chainA,
            DEPLOYER,
            COORDINATOR,
            1,
            mockSrcBridge
        );
        vm.stopPrank();
    }

    /// @dev Tests that receive will fail if the wrong data was provided
    function testReceiveTokensDecodeMismatch() public {
        address mockSrcBridge = address(0xABCDEF);

        vm.prank(COORDINATOR);

        bytes memory wrongData = abi.encode(
            address(123),
            COORDINATOR,
            address(myToken),
            100
        );

        mailbox.putInbox(
            chainB,
            mockSrcBridge,
            COORDINATOR,
            1,
            "SEND",
            wrongData
        );

        vm.startPrank(COORDINATOR);

        vm.expectRevert("The sender should match");
        bridge.receiveTokens(
            chainB,
            chainA,
            DEPLOYER,
            COORDINATOR,
            1,
            mockSrcBridge
        );
        vm.stopPrank();
    }

    /// @dev Tests encode and decode
    function testEncodeDecode() public pure {
        address sender = address(1);
        address receiver = address(2);
        address token = 0x6d19CB7639DeB366c334BD69f030A38e226BA6d2;
        uint256 amount = 100;

        bytes memory data = abi.encode(sender, receiver, token, amount);

        (
            address decodedSender,
            address decodedReceiver,
            address decodedToken,
            uint256 decodedAmount
        ) = abi.decode(data, (address, address, address, uint256));

        assertEq(decodedSender, sender, "Sender should match");
        assertEq(decodedReceiver, receiver, "Receiver should match");
        assertEq(decodedToken, token, "Token should match");
        assertEq(decodedAmount, amount, "Amount should match");
    }
}
