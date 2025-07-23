// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.30;

import { Test } from "forge-std/Test.sol";
import { Mailbox } from "@ssv/src/Mailbox.sol";

contract MailboxTest is Test {
    Mailbox public mailbox;

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
    }
}
