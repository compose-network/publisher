// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {ComposeL2OutputOracle} from "./ComposeL2OutputOracle.sol";
import {Clone} from "@optimism/lib/solady/src/utils/Clone.sol";
import {ISemver} from "interfaces/universal/ISemver.sol";
import {IDisputeGame} from "interfaces/dispute/IDisputeGame.sol";
import {Claim, GameStatus, GameType, Hash, Timestamp} from "@optimism/src/dispute/lib/Types.sol";
import {GameNotInProgress} from "@optimism/src/dispute/lib/Errors.sol";

interface ISP1Verifier {
    function verifyProof(
        bytes32 vkey,
        bytes calldata publicInputs,
        bytes calldata proof
    ) external view;
}



error AlreadyInitialized();

contract ComposeDisputeGame is ISemver, Clone, IDisputeGame {
    uint32 public constant COMPOSE_GAME_TYPE = 5555;

    /// @notice The timestamp of the game's global creation.
    Timestamp public createdAt;

    /// @notice The timestamp of the game's global resolution.
    Timestamp public resolvedAt;

    /// @notice Returns the current status of the game.
    GameStatus public status;

    /// @notice A boolean for whether or not the game type was respected when the game was created.
    bool public wasRespectedGameTypeWhenCreated;

    /// @custom:semver v0.0.1
    string public constant version = "v0.0.1";

    /// @notice The deployed SP1Verifier contract to verify proofs.
    address public constant verifier = 0x17Ef331C3c90E9e5718e81085c721a404eF18436;

    /// @notice The verification key of the aggregation SP1 program.
    bytes32 public constant aggregationVkey = 0x0059ae2f8c8ad61a6af02594067148b58dbecff2e3352170923efda8ea603f1e;

    struct SuperblockAggregationOutputs {
        uint256 superblockNumber; // New head superblock number
        bytes32 parentSuperblockBatchHash; // Hash of the previous superblock
        BootInfoStruct[] bootInfo; // BootInfoStruct, one for each rollup
    }

    struct BootInfoStruct {
        bytes32 l1Head;
        bytes32 l2PreRoot;
        bytes32 l2PostRoot;
        uint64 l2BlockNumber;
        bytes32 rollupConfigHash;
    }

    // ---------------------------------------------------------------------
    // IDisputeGame immutable getters (CWIA layout)
    //
    // DisputeGameFactory.create encodes:
    //   [0, 20):   gameCreator (msg.sender)
    //   [20, 52):  rootClaim
    //   [52, 84):  l1Head (parent hash of creation block)
    //   [84, ...): extraData (opaque)
    // ---------------------------------------------------------------------

    function initialize() external payable {
        if (Timestamp.unwrap(createdAt) != 0) revert AlreadyInitialized();

        createdAt = Timestamp.wrap(uint64(block.timestamp));
        status = GameStatus.IN_PROGRESS;
        wasRespectedGameTypeWhenCreated = true;

        (
            SuperblockAggregationOutputs memory superBlockAggOutputs,
            bytes memory proof
        ) = abi.decode(extraData(), (SuperblockAggregationOutputs, bytes));


        ISP1Verifier(verifier).verifyProof(
            aggregationVkey,
            bytes32ToBytes(sha256(abi.encode(superBlockAggOutputs))),
            proof
        );

        this.resolve();
    }

    /// @dev May only be called if the `status` is `IN_PROGRESS`.
    /// @return status_ The status of the game after resolution.
    function resolve() external returns (GameStatus status_) {
        // INVARIANT: Resolution cannot occur unless the game is currently in progress.
        if (status != GameStatus.IN_PROGRESS) revert GameNotInProgress();

        resolvedAt = Timestamp.wrap(uint64(block.timestamp));
        status_ = GameStatus.DEFENDER_WINS;

        emit Resolved(status = status_);
    }

    /// @return gameType_ The type of proof system being used.
    function gameType() public pure returns (GameType) {
        return GameType.wrap(COMPOSE_GAME_TYPE);
    }

    /// @notice Getter for the creator of the dispute game.
    /// @dev `clones-with-immutable-args` argument #1
    /// @return The creator of the dispute game.
    function gameCreator() public pure returns (address) {
        return _getArgAddress(0x00);
    }

    /// @notice Getter for the root claim.
    /// @dev `clones-with-immutable-args` argument #2
    /// @return The root claim of the DisputeGame.
    function rootClaim() public pure returns (Claim) {
        return Claim.wrap(_getArgBytes32(0x14));
    }

    /// @notice Getter for the parent hash of the L1 block when the dispute game was created.
    /// @dev `clones-with-immutable-args` argument #3
    /// @return The parent hash of the L1 block when the dispute game was created.
    function l1Head() public pure returns (Hash) {
        return Hash.wrap(_getArgBytes32(0x34));
    }

    /// @notice Getter for the extra data.
    /// @dev `clones-with-immutable-args` argument #4
    /// @return extraData_ Any extra data supplied to the dispute game contract by the creator.
    function extraData() public pure returns (bytes memory extraData_) {
        uint256 len;
        assembly {
            // 0x54 is the starting point of the extra data in the calldata.
            // calldataload(sub(calldatasize(), 2)) loads the last 2 bytes of the calldata, which gives the length of the immutable args.
            // shr(240, calldataload(sub(calldatasize(), 2))) masks the last 30 bytes loaded in the previous step, so only the length of the immutable args is left.
            // sub(sub(...)) subtracts the length of the immutable args (2 bytes) and the starting point of the extra data (0x54).
            len := sub(
                sub(shr(240, calldataload(sub(calldatasize(), 2))), 2),
                0x54
            )
        }
        extraData_ = _getArgBytes(0x54, len);
    }

    function gameData()
        external
        pure
        returns (GameType gameType_, Claim rootClaim_, bytes memory extraData_)
    {
        gameType_ = gameType();
        rootClaim_ = rootClaim();
        extraData_ = extraData();
    }

    function l2SequenceNumber()
        external
        pure
        returns (uint256 l2SequenceNumber_)
    {
        return 0;
    }

    function bytes32ToBytes(bytes32 input) public pure returns (bytes memory) {
    bytes memory b = new bytes(32);
    assembly {
        mstore(add(b, 32), input)
    }
    return b;
}
}
