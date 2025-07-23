// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

interface IPingPong {
    function ping(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata data,
        bytes calldata label
    ) external returns (bytes memory message);
    function pong(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata data,
        bytes calldata label
    ) external returns (bytes memory message);
}
