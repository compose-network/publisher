// SPDX-License-Identifier: GPL-3
pragma solidity 0.8.30;

import { IPingPong } from "@ssv/src/interfaces/IPingPong.sol";
import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";

/**
 * @title PingPong
 * @notice This contract lets two blockchain networks talk to each other via maiboxes by sending simple "PING" and "PONG" messages.
 *
 * @author
 * SSV Labs
 */
contract PingPong is IPingPong {

    /// @notice The mailbox used to send and receive messages between chains.
    /// @dev This is set when the contract is created and handles all the cross-chain messaging.
    IMailbox public immutable mailbox;

    /// @notice Sets up the contract with the mailbox address.
    /// @dev This runs when the contract is deployed to connect it to the mailbox.
    /// @param _mailbox The address of the mailbox contract.
    constructor(address _mailbox) {
        mailbox = IMailbox(_mailbox);
    }

    /// @notice Sends a PING message to another chain and checks for a PONG reply.
    /// @dev It writes the PING to the mailbox and reads any PONG that's there.
    /// @param otherChain The ID of the other chain to send to.
    /// @param pongSender The address expected to send the PONG back.
    /// @param pingReceiver The address that should receive the PING on the other chain.
    /// @param sessionId A number to track this specific conversation.
    /// @param data The message data to send with the PING.
    /// @return pongMessage The PONG message data if there is one.
    function ping(
        uint256 otherChain,
        address pongSender,
        address pingReceiver,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory pongMessage) {
        IMailbox(mailbox).write(otherChain, pingReceiver, sessionId, "PING", data);
        pongMessage = IMailbox(mailbox).read(
            otherChain,
            pongSender,
            sessionId,
            "PONG"
        );
        if (pongMessage.length == 0) {
            revert PongMessageEmpty();
        }
    }

    /// @notice Sends a PONG message back to another chain after checking for a PING.
    /// @dev It first reads the PING, then writes the PONG. The sender is automatically this contract.
    /// @param otherChain The ID of the other chain that sent the PING.
    /// @param pingSender The address that sent the PING.
    /// @param sessionId A number to track this specific conversation.
    /// @param data The message data to send with the PONG.
    /// @return pingMessage The PING message data that was read.
    function pong(
        uint256 otherChain,
        address pingSender,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory pingMessage) {
        pingMessage = IMailbox(mailbox).read(
            otherChain,
            pingSender,
            sessionId,
            "PING"
        );
        if (pingMessage.length == 0) {
            revert PingMessageEmpty();
        }
        IMailbox(mailbox).write(otherChain, address(this), sessionId, "PONG", data);
    }
}
