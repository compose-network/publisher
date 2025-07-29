// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { IPingPong } from "@ssv/src/interfaces/IPingPong.sol";
import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";

/**
 * @title PingPong
 * @notice The PingPong Contract to send "PING" and "PONG" messages via CIRC/Espresso.
 *
 * **************
 * ** GLOSSARY **
 * **************
 * @dev The following terms are used throughout the contract:
 *
 *
 * *************
 * ** AUTHORS **
 * *************
 * @author
 * Riccardo Persiani
 */
contract PingPong is IPingPong {
    /// @notice the CIRC Mailbox contract
    IMailbox public mailbox;

    /// @notice constructor to initialize the authorized mailbox
    /// @param _mailbox the address of the mailbox
    constructor(address _mailbox) {
        mailbox = IMailbox(_mailbox);
    }

    error PingMessageEmpty();
    error PongMessageEmpty();

    /// @notice sends a PING message and reads a PONG
    /// @dev messages from the inbox can be read by any contract any number of times.
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param sender address of the sender of the tokens (on the source chain)
    /// @param receiver address of the recipient of the tokens (on the destination chain)
    /// @param sessionId identifier of the user session
    /// @param data the data to write
    /// @return pongMessage the message data
    function ping(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory pongMessage) {
        pongMessage = IMailbox(mailbox).read(
            chainSrc,
            chainDest,
            sender,
            receiver,
            sessionId,
            "PONG"
        );
        if (pongMessage.length == 0) {
            revert PongMessageEmpty();
        }
        IMailbox(mailbox).write(
            chainSrc,
            chainDest,
            receiver,
            sessionId,
            data,
            "PING"
        );
    }

    /// @notice sends a PONG message and reads a PING
    /// @dev any contract can write to the outbox but the source is populated automatically using msg.sender.
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param sender address of the sender of the tokens (on the source chain)
    /// @param receiver address of the recipient of the tokens (on the destination chain)
    /// @param sessionId identifier of the user session
    /// @param data the data to write
    /// @return pingMessage the message data
    function pong(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata data
    ) external returns (bytes memory pingMessage) {
        pingMessage = IMailbox(mailbox).read(
            chainSrc,
            chainDest,
            sender,
            receiver,
            sessionId,
            "PING"
        );
        if (pingMessage.length == 0) {
            revert PingMessageEmpty();
        }
        IMailbox(mailbox).write(
            chainSrc,
            chainDest,
            receiver,
            sessionId,
            data,
            "PONG"
        );
    }
}
