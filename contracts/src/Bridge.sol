// SPDX-License-Identifier: GPL-3
pragma solidity 0.8.30;

import { IBridgeableToken } from "@ssv/src/interfaces/IBridgeableToken.sol";
import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";
import { IBridge } from "@ssv/src/interfaces/IBridge.sol";

/**
 * @title Bridge
 * @notice This contract handles token bridging between blockchain networks using a mailbox for cross-chain messages.
 *
 * @author
 * SSV Labs
 */
contract Bridge is IBridge {

    /// @notice The mailbox contract used for sending and receiving cross-chain messages.
    /// @dev This is set in the constructor and cannot be changed later.
    IMailbox public immutable mailbox;

    /// @notice Initializes the bridge with a mailbox address.
    /// @dev Sets the mailbox interface for all cross-chain operations.
    /// @param _mailbox The address of the mailbox contract.
    constructor(address _mailbox) {
        mailbox = IMailbox(_mailbox);
    }

    /// @notice Sends tokens from the current chain to another chain by burning them here and preparing a message.
    /// @dev The caller must be the tokens sender. Tokens are burned, and a message is written to the mailbox for the destination bridge to process.
    /// @param otherChainId The ID of the destination blockchain.
    /// @param token The address of the token being transferred.
    /// @param sender The address sending the tokens (must be the caller).
    /// @param receiver The address that will receive the tokens on the destination chain.
    /// @param amount The number of tokens to transfer.
    /// @param sessionId A unique ID for this transaction session.
    /// @param destBridge The address of the Bridge contract on the destination chain.
    function send(
        uint256 otherChainId,
        address token,
        address sender,
        address receiver,
        uint256 amount,
        uint256 sessionId,
        address destBridge
    ) external {
        if (msg.sender != sender) {
            revert Unauthorized();
        }

        IBridgeableToken(token).burn(sender, amount);

        bytes memory data = abi.encode(sender, receiver, token, amount);

        mailbox.write(otherChainId, destBridge, sessionId, "SEND", data);

        emit DataWritten(data);
    }

    /// @notice Receives and processes tokens on the destination chain by minting them after reading the source message.
    /// @dev The caller must be the receiver. It checks the message, verifies sender and receiver, mints tokens, and sends an acknowledgment back.
    /// @param otherChainId The ID of the source blockchain.
    /// @param sender The address that sent the tokens from the source chain.
    /// @param receiver The address receiving the tokens (must be the caller).
    /// @param sessionId The unique ID for this transaction session.
    /// @param srcBridge The address of the Bridge contract on the source chain.
    /// @return token The address of the token that was transferred.
    /// @return amount The number of tokens transferred.
    function receiveTokens(
        uint256 otherChainId,
        address sender,
        address receiver,
        uint256 sessionId,
        address srcBridge
    ) external returns (address token, uint256 amount) {
        if (msg.sender != receiver) {
            revert Unauthorized();
        }

        bytes memory m = mailbox.read(
            otherChainId,
            srcBridge,
            sessionId,
            "SEND"
        );

        if (m.length == 0) {
            revert EmptySourceChainMessage();
        }

        address readSender;
        address readReceiver;

        (readSender, readReceiver, token, amount) = abi.decode(
            m,
            (address, address, address, uint256)
        );

        if (readSender != sender) {
            revert SenderMismatch();
        }
        if (readReceiver != receiver) {
            revert ReceiverMismatch();
        }

        IBridgeableToken(token).mint(receiver, amount);

        m = abi.encode("OK");
        mailbox.write(otherChainId, srcBridge, sessionId, "ACK SEND", m);

        emit TokensReceived(token, amount);

        return (token, amount);
    }

    /// @notice Checks for an acknowledgment message from the destination chain.
    /// @dev This is a view function to read the ACK without changing the state.
    /// @param chainDest The ID of the destination blockchain.
    /// @param destBridge The address of the Bridge contract on the destination chain.
    /// @param sessionId The unique ID for the transaction session.
    /// @return The acknowledgment message as bytes, or empty if none exists.
    function checkAck(
        uint256 chainDest,
        address destBridge,
        uint256 sessionId
    ) external view returns (bytes memory) {
        return
            mailbox.read(chainDest, destBridge, sessionId, "ACK SEND");
    }
}
