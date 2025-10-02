// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

interface IPingPong {
    function ping(
        uint256 otherChain,
        address pongSender,
        address pingReceiver,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory message);
    function pong(
        uint256 otherChain,
        address pingSender,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory message);
}
