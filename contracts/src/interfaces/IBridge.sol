// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

interface IBridge {
    function send(
        uint256 chainSrc,
        uint256 chainDest,
        address token,
        address sender,
        address receiver,
        uint256 amount,
        uint256 sessionId
    ) external;
    function receiveTokens(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId
    ) external returns (address token, uint256 amount);
}
