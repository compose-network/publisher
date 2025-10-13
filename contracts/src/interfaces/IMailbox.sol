// SPDX-License-Identifier: GPL-3
pragma solidity 0.8.30;

/**
 * @title IMailbox Interface
 * @notice Defines the structure for a mailbox that handles messages between chains.
 * @dev Includes the message header struct, errors, events, and function signatures.
 *
 * @author
 * SSV Labs
 */
interface IMailbox {

    /// @notice Structure for message details.
    /// @dev Holds all the info needed to identify and route a message.
    struct MessageHeader {
        /// @notice Source chain ID.
        uint256 chainSrc;
        /// @notice Destination chain ID.
        uint256 chainDest;
        /// @notice Sender's address.
        address sender;
        /// @notice Receiver's address.
        address receiver;
        /// @notice Session identifier.
        uint256 sessionId;
        /// @notice Label to differentiate operations.
        bytes label;
    }

    /// @notice Error when the caller is not the coordinator.
    error InvalidCoordinator();

    /// @notice Error when trying to read a message that doesn't exist.
    error MessageNotFound();

    /// @notice Error when the ID is invalid (out of range).
    error InvalidId();

    /// @notice Emitted when a new key is added to the inbox.
    /// @param index The position in the header list.
    /// @param key The message key.
    event NewInboxKey(uint256 indexed index, bytes32 key);

    /// @notice Emitted when a new key is added to the outbox.
    /// @param index The position in the header list.
    /// @param key The message key.
    event NewOutboxKey(uint256 indexed index, bytes32 key);

    /// @notice Function to read a message from the inbox.
    /// @param chainSrc Source chain ID.
    /// @param sender Sender's address.
    /// @param sessionId Session identifier.
    /// @param label Operation label.
    /// @return message The message data.
    function read(
        uint256 chainSrc,
        address sender,
        uint256 sessionId,
        bytes calldata label
    ) external view returns (bytes memory message);

    /// @notice Function to write a message to the outbox.
    /// @param chainDest Destination chain ID.
    /// @param receiver Receiver's address.
    /// @param sessionId Session identifier.
    /// @param label Operation label.
    /// @param data Message data.
    function write(
        uint256 chainDest,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external;

    /// @notice Function to add a message to the inbox (coordinator only).
    /// @param chainSrc Source chain ID.
    /// @param sender Sender's address.
    /// @param receiver Receiver's address.
    /// @param sessionId Session identifier.
    /// @param label Operation label.
    /// @param data Message data.
    function putInbox(
        uint256 chainSrc,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external;
}
