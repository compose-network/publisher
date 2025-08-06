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
    function testEncode() public {
        address sender = 0xA139A1776E60F9645533a9AD419461818D6839a1;
        address receiver = 0xA139A1776E60F9645533a9AD419461818D6839a1;
        address token = 0x6d19CB7639DeB366c334BD69f030A38e226BA6d2;
        uint256 amount = 100;

        bytes memory data = abi.encode(sender, receiver, token, amount);
        // bytes memory data = "";
        (address senderDecoded, , , ) = abi.decode(
            data,
            (address, address, address, uint256)
        );
        assertEq(sender, senderDecoded, "Should match the original sender");
    }
}
