// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

interface IBridge {
    event DataWritten(bytes data);

    event TokensReceived(address token, uint256 amount);

    function send(
        uint256 chainDest,
        address token,
        address sender,
        address receiver,
        uint256 amount,
        uint256 sessionId,
        address destBridge
    ) external;

    function receiveTokens(
        uint256 chainSrc,
        address sender,
        address receiver,
        uint256 sessionId,
        address srcBridge
    ) external returns (address token, uint256 amount);
}
