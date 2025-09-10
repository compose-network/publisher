// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { IToken } from "@ssv/src/interfaces/IToken.sol";
import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";
import { IBridge } from "@ssv/src/interfaces/IBridge.sol";

contract Bridge is IBridge {
    IMailbox public mailbox;

    constructor(address _mailbox) {
        mailbox = IMailbox(_mailbox);
    }

    /// Send some funds to some address on another chain
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param token address of the token to be transferred
    /// @param sender address of the sender of the tokens (on the source chain)
    /// @param receiver address of the recipient of the tokens (on the destination chain)
    /// @param amount amount of tokens to be bridged
    /// @param sessionId identifier of the user session
    /// @param destBridge address of the Bridge contract on the destination chain
    function send(
        uint256 chainSrc, // Source chain ID
        uint256 chainDest, // Destination chain ID
        address token, // Token contract address
        address sender, // Sender's address on source
        address receiver, // Receiver's address on dest
        uint256 amount, // Amount to transfer
        uint256 sessionId, // Session ID for the transfer
        address destBridge // Bridge address on dest chain
    ) external {
        require(sender == msg.sender, "Should be the real sender");

        IToken(token).burn(sender, amount);

        bytes memory data = abi.encode(sender, receiver, token, amount);

        // Send the message to the dest chain
        mailbox.write(chainDest, receiver, sessionId, "SEND", data);

        emit DataWritten(data);
    }

    /// Process funds reception on the destination chain
    /// @param chainSrc source chain identifier the funds are sent from
    /// @param chainDest dest chain identifier the funds are sent to
    /// @param sender address of the sender of the funds
    /// @param receiver address of the receiver of the funds
    /// @param sessionId identifier of the user session
    /// @param srcBridge address of the Bridge contract on the source chain
    /// @return token address of the token that was transferred
    /// @return amount amount of tokens transferred
    function receiveTokens(
        uint256 chainSrc, // Source chain ID
        uint256 chainDest, // Dest chain ID
        address sender, // Original sender
        address receiver, // Receiver on this chain
        uint256 sessionId, // Session ID
        address srcBridge // Bridge on source chain
    ) external returns (address token, uint256 amount) {
        require(msg.sender == receiver, "Only receiver can claim");

        // Read the message from mailbox
        bytes memory m = mailbox.read(
            chainSrc,
            srcBridge,
            receiver,
            sessionId,
            "SEND"
        );

        // If no message, revert
        if (m.length == 0) {
            revert();
        }

        address readSender;
        address readReceiver;

        (readSender, readReceiver, token, amount) = abi.decode(
            m,
            (address, address, address, uint256)
        );

        require(readSender == sender, "The sender should match");
        require(readReceiver == receiver, "The receiver should match");

        IToken(token).mint(receiver, amount);

        m = abi.encode("OK");
        mailbox.write(chainSrc, sender, sessionId, "ACK SEND", m);

        emit TokensReceived(token, amount);

        return (token, amount);
    }

    /// Function to check if ACK is there
    function checkAck(
        uint256 chainSrc, // Source chain
        uint256 chainDest, // Dest chain
        address sender, // Original sender
        address receiver, // Original receiver
        uint256 sessionId, // Session ID
        address destBridge // Dest Bridge address
    ) external view returns (bytes memory) {
        return
            mailbox.read(chainDest, destBridge, sender, sessionId, "ACK SEND");
    }
}
