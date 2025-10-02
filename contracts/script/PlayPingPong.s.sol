// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import "forge-std/Script.sol";
import { Mailbox } from "../src/Mailbox.sol";
import { PingPong } from "../src/PingPong.sol";

contract SimulatePingPong is Script {
    address internal coordinator = vm.envAddress("DEPLOYER_ADDRESS");
    uint256 internal privateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
    string internal rpcA = vm.envString("ROLLUP_A_RPC_URL");
    string internal rpcB = vm.envString("ROLLUP_B_RPC_URL");
    address internal mailboxA = vm.envAddress("MAILBOX_ADDRESS");
    address internal pingPongA = vm.envAddress("PINGPONG_ADDRESS");
    address internal mailboxB = vm.envAddress("MAILBOX_ADDRESS");
    address internal pingPongB = vm.envAddress("PINGPONG_ADDRESS");
    uint256 internal constant ROLLUP_A = 77777;
    uint256 internal constant ROLLUP_B = 88888;
    uint256 internal constant SESSION_ID = 1;
    bytes internal pingData = "Hello from A";
    bytes internal pongData = "Hello from B";

    function run() external {
        // Chain A contracts
        Mailbox mailboxRollupA = Mailbox(mailboxA);
        PingPong pingPongRollupA = PingPong(pingPongA);

        // Chain B contracts
        Mailbox mailboxRollupB = Mailbox(mailboxB);
        PingPong pingPongRollupB = PingPong(pingPongB);

        vm.startBroadcast(privateKey);
        mailboxRollupB.putInbox(ROLLUP_A, pingPongA, pingPongB, SESSION_ID, "PING", pingData);
        pingPongRollupB.pong(ROLLUP_A, pingPongA, SESSION_ID, pongData);
        vm.stopBroadcast();

        vm.startBroadcast(privateKey);
        mailboxRollupA.putInbox(ROLLUP_B, pingPongB, pingPongA, SESSION_ID, "PONG", pongData);
        pingPongRollupA.ping(ROLLUP_B, pingPongB, pingPongB, SESSION_ID, pingData);
        vm.stopBroadcast();
    }
}