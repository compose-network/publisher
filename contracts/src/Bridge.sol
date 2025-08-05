// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { IToken } from "@ssv/src/interfaces/IToken.sol";
import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";

contract Bridge {
    IMailbox public mailbox;

    constructor(address _mailbox) {
        mailbox = IMailbox(_mailbox);
    }

    // Send some funds to some address on another chain
    // @param chainDest identifier of the destination chain
    // @param token address of the token to be transferred
    // @param sender address of the sender of the tokens (on the source chain)
    // @param receiver address of the recipient of the tokens (on the destination chain)
    // @param amount amount of tokens to be bridged
    // @param sessionId identifier of the user session
    // @param label label to be able to differentiate between different bridge operations within a same session.
    function send(
        uint256 chainSrc,
        uint256 chainDest,
        address token,
        address sender,
        address receiver,
        uint256 amount,
        uint256 sessionId
    ) external {
        require(sender == msg.sender, "Should be the real sender");
        // Burn the assets
        IToken(token).burn(sender, amount);

        // Write to the outbox
        mailbox.write(
            chainSrc,
            chainDest,
            receiver,
            sessionId,
            abi.encode(sender, receiver, token, amount),
            "SEND"
        );

        // Check the funds have been received on the other chain
        bytes memory m = mailbox.read(
            chainSrc,
            chainDest,
            receiver,
            sessionId,
            "ACK SEND"
        );
        if (m.length == 0) {
            revert();
        }
    }

    /// Process funds reception on the destination chain
    /// @param chainSrc source chain identifier the funds are sent from
    /// @param chainDest dest chain identifier the funds are sent to
    /// @param sender address of the sender of the funds
    /// @param receiver address of the receiver of the funds
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different bridge operations within a same session.
    /// @return token address of the token that was transferred
    /// @return amount amount of tokens transferred
    function receive(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label
    ) external returns (address token, uint256 amount) {
        string memory labelStr = string(label);

        // Fetch the message
        bytes memory m = mailbox.read(
            chainSrc,
            chainDest,
            receiver,
            sessionId,
            bytes(string.concat(labelStr, "SEND"))
        );
        // Check the message is valid
        if (m.length == 0) {
            revert();
        }

        // Mint the assets
        address readSender;
        address readReceiver;

        (sender, receiver, token, amount) = abi.decode(
            m,
            (address, address, address, uint256)
        );

        require(readSender == sender, "The sender should match");
        require(readReceiver == receiver, "The receiver should match");

        IToken(token).mint(receiver, amount);

        // Acknowledge the reception of funds
        m = abi.encode("OK");
        mailbox.write(
            chainSrc,
            chainDest,
            sender,
            sessionId,
            bytes(string.concat(labelStr, "ACK SEND")),
            m
        );

        return (token, amount);
    }
}
