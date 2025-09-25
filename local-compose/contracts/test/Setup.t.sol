// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Test } from "forge-std/Test.sol";
import { Mailbox } from "@ssv/src/Mailbox.sol";
import { PingPong } from "@ssv/src/PingPong.sol";
import { MyToken } from "@ssv/src/Token.sol";
import { Bridge } from "@ssv/src/Bridge.sol";

contract Setup is Test {
    Mailbox public mailbox;
    PingPong public pingPong;
    MyToken public myToken;
    Bridge public bridge;

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
        myToken = new MyToken();
        bridge = new Bridge(address(mailbox));

        vm.label(address(mailbox), "Mailbox");
        vm.label(address(pingPong), "PingPong");
        vm.label(address(myToken), "MyToken");
        vm.label(address(bridge), "Bridge");
    }
}
