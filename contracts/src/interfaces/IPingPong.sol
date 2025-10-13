// SPDX-License-Identifier: GPL-3
pragma solidity 0.8.30;

/**
 * @title IPingPong Interface
 * @notice Defines the functions for sending PING and PONG messages between chains.
 *
 * @author
 * SSV Labs
 */
interface IPingPong {

    /// @notice Error thrown if there's no PING message to read.
    error PingMessageEmpty();
    /// @notice Error thrown if there's no PONG message to read.
    error PongMessageEmpty();

    /// @notice Function to send a PING and get a PONG.
    /// @param otherChain The other chain's ID.
    /// @param pongSender Address sending the PONG.
    /// @param pingReceiver Address receiving the PING.
    /// @param sessionId Conversation tracker.
    /// @param data Message data.
    /// @return message The PONG data.
    function ping(
        uint256 otherChain,
        address pongSender,
        address pingReceiver,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory message);

    /// @notice Function to send a PONG after getting a PING.
    /// @param otherChain The other chain's ID.
    /// @param pingSender Address that sent the PING.
    /// @param sessionId Conversation tracker.
    /// @param data Message data.
    /// @return message The PING data.
    function pong(
        uint256 otherChain,
        address pingSender,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory message);
}
