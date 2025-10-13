// SPDX-License-Identifier: GPL-3
pragma solidity 0.8.30;

/**
 * @title IBridge Interface
 * @notice Defines the structure for a bridge contract that transfers tokens between chains.
 *
 * @author
 * SSV Labs
 */
interface IBridge {

    /// @notice Error thrown when the caller is not authorized (not the sender or receiver).
    error Unauthorized();

    /// @notice Error thrown when there is no message from the source chain.
    error EmptySourceChainMessage();

    /// @notice Error thrown when the sender in the message does not match the expected sender.
    error SenderMismatch();

    /// @notice Error thrown when the receiver in the message does not match the expected receiver.
    error ReceiverMismatch();

    /// @notice Emitted when data is written to the mailbox for cross-chain transfer.
    /// @param data The encoded data sent in the message.
    event DataWritten(bytes data);

    /// @notice Emitted when tokens are successfully received and minted on the destination chain.
    /// @param token The address of the token received.
    /// @param amount The amount of tokens received.
    event TokensReceived(address token, uint256 amount);


    /// @notice Function to send tokens to another chain.
    /// @param chainDest The ID of the destination chain.
    /// @param token The address of the token to send.
    /// @param sender The sender's address.
    /// @param receiver The receiver's address on the destination chain.
    /// @param amount The amount of tokens to send.
    /// @param sessionId The session ID for tracking.
    /// @param destBridge The bridge address on the destination chain.
    function send(
        uint256 chainDest,
        address token,
        address sender,
        address receiver,
        uint256 amount,
        uint256 sessionId,
        address destBridge
    ) external;


    /// @notice Function to receive tokens from another chain.
    /// @param chainSrc The ID of the source chain.
    /// @param sender The sender's address from the source chain.
    /// @param receiver The receiver's address.
    /// @param sessionId The session ID for tracking.
    /// @param srcBridge The bridge address on the source chain.
    /// @return token The token address received.
    /// @return amount The amount received.
    function receiveTokens(
        uint256 chainSrc,
        address sender,
        address receiver,
        uint256 sessionId,
        address srcBridge
    ) external returns (address token, uint256 amount);
}
