// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {ComposeL2OutputOracle} from "./ComposeL2OutputOracle.sol";
import {Clone} from "@optimism/lib/solady/src/utils/Clone.sol";
import {ISemver} from "interfaces/universal/ISemver.sol";
import {IDisputeGame} from "interfaces/dispute/IDisputeGame.sol";
import {Claim, GameStatus, GameType, GameTypes, Hash, Timestamp} from "@optimism/src/dispute/lib/Types.sol";
import {GameNotInProgress, OutOfOrderResolution} from "@optimism/src/dispute/lib/Errors.sol";

abstract contract ComposeDisputeGame is ISemver, Clone, IDisputeGame {
    struct SuperblockAggregationOutputs {
        uint256 superblockNumber;
        bytes32 parentSuperblockBatchHash;
        BootInfoStruct[] bootInfo;
    }

    struct BootInfoStruct {
        bytes32 l1Head;
        bytes32 l2PreRoot;
        bytes32 l2PostRoot;
        uint64 l2BlockNumber;
        bytes32 rollupConfigHash;
    }

    address internal immutable L2_OUTPUT_ORACLE;

    /// @notice The timestamp of the game's global creation.
    Timestamp public createdAt;

    /// @notice The timestamp of the game's global resolution.
    Timestamp public resolvedAt;

    /// @notice Returns the current status of the game.
    GameStatus public status;

    /// @notice A boolean for whether or not the game type was respected when the game was created.
    bool public wasRespectedGameTypeWhenCreated;

    /// @custom:semver v0.1.0
    string public constant version = "v0.1.0";

    // ---------------------------------------------------------------------
    // IDisputeGame immutable getters (CWIA layout)
    //
    // DisputeGameFactory.create encodes:
    //   [0, 20):   gameCreator (msg.sender)
    //   [20, 52):  rootClaim
    //   [52, 84):  l1Head (parent hash of creation block)
    //   [84, ...): extraData (opaque)
    // ---------------------------------------------------------------------

    /** extraData layout
     *  (SuperblockAggregationOutputs superBlockAggregationOutputs, bytes proof)
     *
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
     */

    constructor(address _l2OutputOracle) {
        L2_OUTPUT_ORACLE = _l2OutputOracle;
    }

    function initialize() external payable {
        createdAt = Timestamp.wrap(uint64(block.timestamp));
        status = GameStatus.IN_PROGRESS;
        wasRespectedGameTypeWhenCreated = true;

        ComposeL2OutputOracle oracle = ComposeL2OutputOracle(
            L2_OUTPUT_ORACLE
        );

        oracle.proposeL2Output(
            rootClaim().raw(),
            l1BlockNumber(),
            extraData()
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
        return GameTypes.COMPOSE_VALIDITY; // 5555
    }

    /// @notice Getter for the root claim.
    /// @dev `clones-with-immutable-args` argument #2
    /// @return The root claim of the DisputeGame.
    function rootClaim() public pure returns (Claim) {
        return Claim.wrap(_getArgBytes32(0x14));
    }

    function l1BlockNumber() public pure returns (uint256 l1BlockNumber_) {
        l1BlockNumber_ = _getArgUint256(0x74);
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
}
