// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

interface IMailbox {
    function read(
        uint256 chainSrc,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label
    ) external view returns (bytes memory message);
    function write(
        uint256 chainDest,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external;
    function putInbox(
        uint256 chainSrc,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external;
}
