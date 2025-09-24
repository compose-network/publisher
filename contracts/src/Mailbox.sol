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
    /*
     * STORAGE KEYS:
     *
     *   solidity storage consists of slots, each slot is 32 bytes. each slot has a number,
     *   numbers are distributed sequentially for simple data types and in harder way for complex
     *
     *   See https://docs.soliditylang.org/en/latest/internals/layout_in_storage.html
     *       https://ethereum.stackexchange.com/questions/133473/how-to-calculate-the-location-index-slot-in-storage-of-a-mapping-key
     *       https://medium.com/@flores.eugenio03/exploring-the-storage-layout-in-solidity-and-how-to-access-state-variables-bf2cbc6f8018
     *
     *   --- how to compute slots ---
     *
     *   simple vars:
     *       sequentially assigned (variable1 = slot 0 (0x0), variable2 = slot 1 (0x1), etc.)
     *
     *   mapping:
     *       mapping(a => b) someMapping is declared at slot N
     *       storage key for someMapping[k] = keccak256(abi.encode(a, N))
     *       ex: `inbox[0x123...]` stored at keccak256(0x123..., 2) since `inbox` is the 3rd var
     *
     *   dynamic arrays:
     *       SomeType[] array at slot N
     *       slot N stores array length (for array [1,2,3] slot N value would be equal to 3)
     *       elements start at base = keccak256(abi.encode(N))
     *       element i is at base + i
     *       ex: `keyListInbox[0]` stored at keccak256(abi.encode(5)) + 0
     *
     *        once you know the slot formula, you can pass the 32-byte
     *        storage key to eth_getProof to fetch proofs for that variable
     *
     *   Feel free to ping me if you have any questions :)
     */

    /// @notice
    address public immutable COORDINATOR;
    /// Chain-specific inbox and outbox roots
    /// @notice List of chain IDs with messages in the inbox
    uint256[] public chainIDsInbox;
    /// @notice List of chain IDs with messages in the outbox
    uint256[] public chainIDsOutbox;
    /// @notice Mapping of chain ID to inbox root
    mapping(uint256 chainId => bytes32 inboxRoot) public inboxRootPerChain;
    /// @notice Mapping of chain ID to outbox root
    mapping(uint256 chainId => bytes32 outboxRoot) public outboxRootPerChain;
    /// @notice
    mapping(bytes32 key => bytes message) public inbox;
    /// @notice
    mapping(bytes32 key => bytes message) public outbox;
    /// @notice
    mapping(bytes32 key => bool used) public createdKeys;
    /// @notice
    MessageHeader[] public messageHeaderListInbox;
    /// @notice
    MessageHeader[] public messageHeaderListOutbox;

    modifier onlyCoordinator() {
        if (msg.sender != COORDINATOR) revert InvalidCoordinator();
        _;
    }

    /// @notice constructor to initialize the authorized coordinator and chain ID
    /// @param _coordinator the address of the coordinator
    constructor(address _coordinator) {
        COORDINATOR = _coordinator;
    }

    /// @notice creates the key for the inbox and outbox
    /// @param chainMessageSender identifier of the source chain
    /// @param chainMessageRecipient identifier of the destination chain
    /// @param sender address of the sender
    /// @param receiver address of the recipient
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @return key the key in the form of a hash
    function getKey(
        uint256 chainMessageSender,
        uint256 chainMessageRecipient,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label
    ) public pure returns (bytes32 key) {
        key = keccak256(
            abi.encodePacked(
                chainMessageSender,
                chainMessageRecipient,
                sender,
                receiver,
                sessionId,
                label
            )
        );
    }

    /// @notice read messages from the inbox
    /// @dev messages from the inbox can be read by any contract any number of times.
    /// @param chainMessageSender identifier of the chain that sent a message
    /// @param sender address of the sender on the source chain
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @return message the message data
    function read(
        uint256 chainMessageSender,
        address sender,
        uint256 sessionId,
        bytes calldata label
    ) external view returns (bytes memory message) {
        bytes32 key = getKey(
            chainMessageSender, // other chain is the sender
            block.chainid, // this chain is receiver
            sender,
            msg.sender,
            sessionId,
            label
        );

        if (inbox[key].length == 0 && !createdKeys[key]) {
            revert MessageNotFound();
        }

        return inbox[key];
    }

    /// @notice write messages to the outbox
    /// @dev any contract can write to the outbox; sender is populated automatically using msg.sender.
    /// @param chainMessageRecipient identifier of the destination chain
    /// @param receiver address of the recipient on the destination chain
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @param data the data to write
    function write(
        uint256 chainMessageRecipient,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external {
        bytes32 key = getKey(
            block.chainid,  // this chain is the message sender
            chainMessageRecipient, // other chain is the recipient
            msg.sender,
            receiver,
            sessionId,
            label
        );
        outbox[key] = data;
        createdKeys[key] = true;
        messageHeaderListOutbox.push(
            MessageHeader(
                block.chainid,
                chainMessageRecipient,
                msg.sender,
                receiver,
                sessionId,
                label
            )
        );

        emit NewOutboxKey(messageHeaderListOutbox.length - 1, key);

        // Update chain-specific outbox root
        if (outboxRootPerChain[chainMessageRecipient] == bytes32(0)) {
            chainIDsOutbox.push(chainMessageRecipient);
        }
        outboxRootPerChain[chainMessageRecipient] = keccak256(
            abi.encode(outboxRootPerChain[chainMessageRecipient], key, data)
        );
    }

    /// @notice write messages to the inbox - onlyCoordinator
    /// @dev the inboxes are filled with the messages computed by the Coordinator.
    /// @param chainMessageSender identifier of the source chain
    /// @param sender address of the sender on the source chain
    /// @param receiver address of the recipient on the destination chain
    /// @param sessionId identifier of the user session
    /// @param label label to be able to differentiate between different operations within a same session.
    /// @param data the data to write
    function putInbox(
        uint256 chainMessageSender,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external onlyCoordinator {
        bytes32 key = getKey(
            chainMessageSender,  // message is incoming from another chain
            block.chainid,  // this chain is the message recipient
            sender,
            receiver,
            sessionId,
            label
        );
        inbox[key] = data;
        createdKeys[key] = true;
        messageHeaderListInbox.push(
            MessageHeader(chainMessageSender, block.chainid, sender, receiver, sessionId, label)
        );

        emit NewInboxKey(messageHeaderListInbox.length - 1, key);

        // Update chain-specific inbox root
        if (inboxRootPerChain[chainMessageSender] == bytes32(0)) {
            chainIDsInbox.push(chainMessageSender);
        }
        inboxRootPerChain[chainMessageSender] = keccak256(
            abi.encode(inboxRootPerChain[chainMessageSender], key, data)
        );
    }

    function computeKey(uint256 id) external view returns (bytes32) {
        require(id < messageHeaderListInbox.length, "Invalid id");

        MessageHeader storage m = messageHeaderListInbox[id];

        return keccak256(
                abi.encodePacked(
                    m.chainSrc,
                    m.chainDest,
                    m.sender,
                    m.receiver,
                    m.sessionId,
                    m.label
                )
            );
    }

    // @dev only for testing - uncomment and use to clear the storage
    /**
    function clear() public onlyCoordinator {
        for (uint256 i = 0; i < messageHeaderListInbox.length; i++) {
            MessageHeader storage m = messageHeaderListInbox[i];

            bytes32 key = keccak256(
                abi.encodePacked(
                    m.chainSrc,
                    m.chainDest,
                    m.sender,
                    m.receiver,
                    m.sessionId,
                    m.label
                )
            );
            delete inbox[key];
            delete createdKeys[key];
            delete messageHeaderListInbox[i];
        }
        delete messageHeaderListInbox;

        for (uint256 j = 0; j < messageHeaderListOutbox.length; j++) {
            MessageHeader storage m2 = messageHeaderListOutbox[j];

            bytes32 key2 = keccak256(
                abi.encodePacked(
                    m2.chainSrc,
                    m2.chainDest,
                    m2.sender,
                    m2.receiver,
                    m2.sessionId,
                    m2.label
                )
            );
            delete outbox[key2];
            delete createdKeys[key2];
            delete messageHeaderListOutbox[j];
        }
        delete messageHeaderListOutbox;

        // Clear chain-specific roots and arrays
        for (uint256 i = 0; i < chainIDsInbox.length; i++) {
            delete inboxRootPerChain[chainIDsInbox[i]];
        }
        delete chainIDsInbox;

        for (uint256 j = 0; j < chainIDsOutbox.length; j++) {
            delete outboxRootPerChain[chainIDsOutbox[j]];
        }
        delete chainIDsOutbox;
    }
    */
}