// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { IMailbox } from "@ssv/src/interfaces/IMailbox.sol";
import { console } from "forge-std/console.sol";

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
 * SSV Labs
 */
contract Mailbox is IMailbox {
    /// @notice
    address public coordinator;
    /// @notice The chain ID of this rollup.
    uint256 public chainId;
    /// @notice
    mapping(bytes32 key => bytes message) public inbox;
    /// @notice
    mapping(bytes32 key => bytes message) public outbox;
    /// @notice
    mapping(bytes32 key => bool used) public createdKeys;
    /// @notice
    bytes32[] public keyListInbox;
    /// @notice
    bytes32[] public keyListOutbox;
    /// @notice Incremental digest for inbox, updated on putInbox.
    bytes32 public inboxRoot;
    /// @notice Incremental digest for outbox, updated on write.
    bytes32 public outboxRoot;

    error InvalidCoordinator();
    error MessageNotFound();

    modifier onlyCoordinator() {
        if (msg.sender != coordinator) revert InvalidCoordinator();
        _;
    }

    /// @notice constructor to initialize the authorized coordinator and chain ID
    /// @param _coordinator the address of the coordinator
    /// @param _chainId the ID of this chain/rollup
    constructor(address _coordinator, uint256 _chainId) {
        coordinator = _coordinator;
        chainId = _chainId;
    }

    function clear() public onlyCoordinator {
        for (uint256 i = 0; i < keyListInbox.length; i++) {
            bytes32 key = keyListInbox[i];
            delete inbox[key];
        }
        for (uint256 i = 0; i < keyListOutbox.length; i++) {
            bytes32 key = keyListOutbox[i];
            delete outbox[key];
        }
        // reset the keys too
        delete keyListInbox;
        delete keyListOutbox;

        inboxRoot = 0;
        outboxRoot = 0;
    }

    /// @notice creates the key for the inbox and outbox
    /// @param chainSrc identifier of the source chain
    /// @param chainDest identifier of the destination chain
    /// @param sender address of the sender
    /// @param receiver address of the recipient
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
    ) public pure returns (bytes32 key) {
        key = keccak256(
            abi.encodePacked(chainSrc, chainDest, sender, receiver, sessionId, label)
        );
    }

    /// @notice read messages from the inbox
    /// @dev messages from the inbox can be read by any contract any number of times.
    /// @param chainSrc identifier of the source chain
    /// @param sender address of the sender on the source chain
    /// @param receiver address of the recipient on the destination chain
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @return message the message data
    function read(
        uint256 chainSrc,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label
    ) external view returns (bytes memory message) {
        bytes32 key = getKey(chainSrc, chainId, sender, receiver, sessionId, label);

        if (inbox[key].length == 0 && !createdKeys[key]) {
            revert MessageNotFound();
        }

        return inbox[key];
    }

    /// @notice write messages to the outbox
    /// @dev any contract can write to the outbox; sender is populated automatically using msg.sender.
    /// @param chainDest identifier of the destination chain
    /// @param receiver address of the recipient on the destination chain
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @param data the data to write
    function write(
        uint256 chainDest,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external {
        address sender = msg.sender;
        uint256 chainSrc = chainId;
        bytes32 key = getKey(chainSrc, chainDest, sender, receiver, sessionId, label);
        outbox[key] = data;
        createdKeys[key] = true;
        keyListOutbox.push(key);
        // update incremental digest
        outboxRoot = keccak256(abi.encode(outboxRoot, key, data));
    }

    /// @notice write messages to the inbox - onlyCoordinator
    /// @dev the inboxes are filled with the messages computed by the Coordinator.
    /// @param chainSrc identifier of the source chain
    /// @param sender address of the sender on the source chain
    /// @param receiver address of the recipient on the destination chain
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @param data the data to write
    function putInbox(
        uint256 chainSrc,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external onlyCoordinator {
        bytes32 key = getKey(chainSrc, chainId, sender, receiver, sessionId, label);
        inbox[key] = data;
        createdKeys[key] = true;
        keyListInbox.push(key);
        // update incremental digest
        inboxRoot = keccak256(abi.encode(inboxRoot, key, data));
    }
}
