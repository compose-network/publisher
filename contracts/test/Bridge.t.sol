// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Setup } from "@ssv/test/Setup.t.sol";
import { console } from "forge-std/console.sol";

contract BridgeTest is Setup {
    function testSend() public {
        vm.prank(COORDINATOR);
        mailbox.putInbox(2, 1, COORDINATOR, 1, "test message", "ACK SEND");
        bytes32 key = mailbox.getKey(2, 1, COORDINATOR, 1, "ACK SEND");
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
        address sender = address(DEPLOYER);
        address receiver = address(COORDINATOR);
        address token = 0x2e234DAe75C793f67A35089C9d99245E1C58470b;
        uint256 amount = 100;
        bytes memory data = abi.encode(sender, receiver, token, amount);
        (
            address readSender,
            address readReceiver,
            address decodedToken,
            uint256 decodedAmount
        ) = abi.decode(data, (address, address, address, uint256));
        assertEq(readSender, sender, "should match");
        mailbox.putInbox(1, 2, COORDINATOR, 1, data, "SEND");
        bytes32 key = mailbox.getKey(1, 2, COORDINATOR, 1, "SEND");
        assertEq(mailbox.inbox(key), data, "The message should match");

        vm.startPrank(DEPLOYER);
        (address receivedToken, uint256 receivedAmount) = bridge.receiveTokens(
            1,
            2,
            DEPLOYER,
            COORDINATOR,
            1
        );
        assertEq(receivedToken, token, "should match the token");
    }

    function testEncode() public pure {
        address sender = 0xA139A1776E60F9645533a9AD419461818D6839a1;
        address receiver = 0xA139A1776E60F9645533a9AD419461818D6839a1;
        address token = 0x6d19CB7639DeB366c334BD69f030A38e226BA6d2;
        uint256 amount = 100;

        bytes memory data = abi.encode(sender, receiver, token, amount);
        console.logBytes(data);

        // bytes memory data = "";
        (address senderDecoded, , , ) = abi.decode(
            data,
            (address, address, address, uint256)
        );
        assertEq(sender, senderDecoded, "Should match the original sender");
    }
}
