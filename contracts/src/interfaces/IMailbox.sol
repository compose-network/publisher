// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

interface IMailbox {
    // Header for message
    struct MessageHeader {
        uint256 chainSrc;
        uint256 chainDest;
        address sender;
        address receiver;
        uint256 sessionId;
        bytes label;
    }

    error InvalidCoordinator();

    error MessageNotFound();

    event NewInboxKey(uint256 indexed index, bytes32 key);

    event NewOutboxKey(uint256 indexed index, bytes32 key);

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
