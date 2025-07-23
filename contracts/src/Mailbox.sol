// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";

/**
 * @title Mailbox
 * @notice The Mailbox Contract to manage chain interaction via CIRC/Espresso.
 *
 * **************
 * ** GLOSSARY **
 * **************
 * @dev The following terms are used throughout the contract:
 *
 * - **Coordinator**: a.k.a. the Shared Sequencer:
 *   1. pre-populates all cross-chain messages in the chain inboxes.
 *
 * *************
 * ** AUTHORS **
 * *************
 * @author
 * Riccardo Persiani
 */
contract Mailbox is IMailbox {
    /// @notice
    address public coordinator;
    /// @notice
    mapping(bytes32 key => bytes message) public inbox;
    /// @notice
    mapping(bytes32 key => bytes message) public outbox;
    /// @notice
    bytes32[] public keyListInbox;
    /// @notice
    bytes32[] public keyListOutbox;

    error InvalidCoordinator();

    modifier onlyCoordinator() {
        if (msg.sender != coordinator) revert InvalidCoordinator();
        _;
    }

    /// @notice constructor to initialized the authorized coordinator
    /// @param _coordinator the address of the coordinator
    constructor(address _coordinator) {
        coordinator = _coordinator;
    }

    /// @notice creates the key for the inbox and outbox
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param sender address of the sender of the tokens (on the source chain)
    /// @param receiver address of the recipient of the tokens (on the destination chain)
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @return key the key in the form of a hash
    function getKey(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label
    ) internal pure returns (bytes32 key) {
        key = keccak256(
            (
                abi.encodePacked(
                    chainSrc,
                    chainDest,
                    sender,
                    receiver,
                    sessionId,
                    label
                )
            )
        );
    }

    /// @notice read messages from the inbox
    /// @dev messages from the inbox can be read by any contract any number of times.
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param sender address of the sender of the tokens (on the source chain)
    /// @param receiver address of the recipient of the tokens (on the destination chain)
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @return message the message data
    function read(
        uint256 chainSrc,
        uint256 chainDest,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label
    ) external view returns (bytes memory message) {
        bytes32 key = getKey(
            chainSrc,
            chainDest,
            sender,
            receiver,
            sessionId,
            label
        );

        return inbox[key];
    }

    /// @notice write messages to the outbox
    /// @dev any contract can write to the outbox but the source is populated automatically using msg.sender.
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param receiver address of the recipient of the tokens (on the destination chain)
    /// @param sessionId identifier of the user session
    /// @param data the data to write
    /// @param label label to be able to differentiate between different operations within a same session.
    function write(
        uint256 chainSrc,
        uint256 chainDest,
        address receiver,
        uint256 sessionId,
        bytes calldata data,
        bytes calldata label
    ) external {
        bytes32 key = getKey(
            chainSrc,
            chainDest,
            msg.sender,
            receiver,
            sessionId,
            label
        );
        outbox[key] = data;
        keyListOutbox.push(key);
    }

    /// @notice write messages to the inbox - onlyCoordinator
    /// @dev the inboxes are filled with the messages computed by the Coordinator.
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param receiver address of the recipient of the tokens (on the destination chain)
    /// @param sessionId identifier of the user session
    /// @param data the data to write
    /// @param label label to be able to differentiate between different operations within a same session.
    function putInbox(
        uint256 chainSrc,
        uint256 chainDest,
        address receiver,
        uint256 sessionId,
        bytes calldata data,
        bytes calldata label
    ) external onlyCoordinator {
        bytes32 key = getKey(
            chainSrc,
            chainDest,
            msg.sender,
            receiver,
            sessionId,
            label
        );
        inbox[key] = data;
        keyListInbox.push(key);
    }
}
