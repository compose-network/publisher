// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;
import {ComposeL2OutputOracle} from "./ComposeL2OutputOracle.sol";
import {Clone} from "@optimism/lib/solady/src/utils/Clone.sol";
import {ISemver} from "interfaces/universal/ISemver.sol";
import {IDisputeGame} from "interfaces/dispute/IDisputeGame.sol";
import {Claim, GameStatus, GameType, GameTypes, Hash, Timestamp} from "@optimism/src/dispute/lib/Types.sol";
import {GameNotInProgress, OutOfOrderResolution} from "@optimism/src/dispute/lib/Errors.sol";

interface ISP1Verifier {
    function verifyProof(bytes32 vkey, bytes calldata publicInputs, bytes calldata proof) external view returns (bool);
}

error AlreadyInitialized();

contract ComposeDisputeGame is ISemver, Clone, IDisputeGame {
    uint32 public constant COMPOSE_GAME_TYPE = 5555;
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
    struct DecodedExtraData {
        bytes superRootPreimage;
        bytes aggregatedProof;
        bool asr;
        address sp1Verifier;
        bytes32 aggregationVkey;
        bytes32 cohortCommitment;
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
    /// @custom:semver v0.0.1
    string public constant version = "v0.0.1";
    // ---------------------------------------------------------------------
    // IDisputeGame immutable getters (CWIA layout)
    //
    // DisputeGameFactory.create encodes:
    // [0, 20): gameCreator (msg.sender)
    // [20, 52): rootClaim
    // [52, 84): l1Head (parent hash of creation block)
    // [84, ...): extraData (opaque)
    // ---------------------------------------------------------------------
    /** extraData layout
     * (SuperblockAggregationOutputs superBlockAggregationOutputs, bytes proof)
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
        if (Timestamp.unwrap(createdAt) != 0) revert AlreadyInitialized();

        bytes memory _extraData = extraData();
        DecodedExtraData memory decoded = abi.decode(_extraData, (DecodedExtraData));

        wasRespectedGameTypeWhenCreated = decoded.asr;

        require(keccak256(decoded.superRootPreimage) == rootClaim().raw(), "Invalid root preimage");

        SuperblockAggregationOutputs memory outputs = abi.decode(decoded.superRootPreimage, (SuperblockAggregationOutputs));

        bytes32 _l1Head = l1Head().raw();

        require(_l1Head == blockhash(block.number - 1), "The l1 head must be the parent of the creation block.");

        require(ISP1Verifier(decoded.sp1Verifier).verifyProof(decoded.aggregationVkey, abi.encode(rootClaim().raw(), decoded.cohortCommitment, _l1Head), decoded.aggregatedProof), "Invalid proof");

        createdAt = Timestamp.wrap(uint64(block.timestamp));
        status = GameStatus.IN_PROGRESS;

        ComposeL2OutputOracle oracle = ComposeL2OutputOracle(
            L2_OUTPUT_ORACLE
        );
        oracle.proposeL2Output(
            rootClaim().raw(),
            outputs.superblockNumber,
            _l1Head,
            block.number - 1,
            _extraData
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

    function gameCreator() public pure returns (address) {
        return _getArgAddress(0x00);
    }

    function rootClaim() public pure returns (Claim) {
        return Claim.wrap(_getArgBytes32(0x14));
    }

    function l1Head() public pure returns (Hash) {
        return Hash.wrap(_getArgBytes32(0x34));
    }

    function l2SequenceNumber() external pure returns (uint256 l2SequenceNumber_) {
        return 0;
    }

    /// @notice Getter for the extra data.
    /// @dev `clones-with-immutable-args` argument #4
    /// @return extraData_ Any extra data supplied to the dispute game contract by the creator.
    function extraData() public pure returns (bytes memory extraData_) {
        uint256 len;
        assembly {
            len := sub(
                sub(shr(240, calldataload(sub(calldatasize(), 2))), 2),
                0x54
            )
        }
        extraData_ = _getArgBytes(0x54, len);
    }

    function gameData() external view returns (GameType gameType_, Claim rootClaim_, bytes memory extraData_) {
        gameType_ = gameType();
        rootClaim_ = rootClaim();
        extraData_ = extraData();
    }
}