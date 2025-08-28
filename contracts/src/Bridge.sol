// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { IToken } from "@ssv/src/interfaces/IToken.sol";
import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";
import { IBridge } from "@ssv/src/interfaces/IBridge.sol";

contract Bridge is IBridge {
    IMailbox public mailbox;

    event EmptyEvent();
    event DataWritten(bytes data);

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

        bytes memory data = abi.encode(sender, receiver, token, amount);

        // Write to the outbox
        mailbox.write(
            chainSrc,
            receiver,
            sessionId,
            "SEND",
            data
        );

        emit DataWritten(data);

        // Check the funds have been received on the other chain
        bytes memory m = mailbox.read(
            chainDest,
            sender,
            receiver,
            sessionId,
            "ACK SEND"
        );
        if (m.length == 0) {
            //emit EmptyEvent();
            revert();
        }
    }

    /// Process funds reception on the destination chain
    /// @param chainSrc source chain identifier the funds are sent from
    /// @param chainDest dest chain identifier the funds are sent to
    /// @param sender address of the sender of the funds
    /// @param receiver address of the receiver of the funds
    /// @param sessionId identifier of the user session
    /// @return token address of the token that was transferred
    /// @return amount amount of tokens transferred
    function receiveTokens(
        uint256 chainSrc, // a
        uint256 chainDest, // b
        address sender,
        address receiver,
        uint256 sessionId
    ) external returns (address token, uint256 amount) {
        // Fetch the message
        bytes memory m = mailbox.read(
            chainSrc,
            sender,
            receiver,
            sessionId,
            "SEND"
        );
        // Check the message is valid
        if (m.length == 0) {
            revert();
            //emit EmptyEvent();
        }

        // Mint the assets
        address readSender;
        address readReceiver;

        (readSender, readReceiver, token, amount) = abi.decode(
            m,
            (address, address, address, uint256)
        );
        // amount = 100;
        // token = address(0x6d19CB7639DeB366c334BD69f030A38e226BA6d2);

        require(readSender == sender, "The sender should match");
        require(readReceiver == receiver, "The receiver should match");

        IToken(token).mint(receiver, amount);

        // Acknowledge the reception of funds
        m = abi.encode("OK");
        mailbox.write(
            chainDest,
            receiver,
            sessionId,
            "ACK SEND",
            m
        );

        return (token, amount);
    }
}
