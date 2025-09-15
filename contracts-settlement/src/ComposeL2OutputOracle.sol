// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import {Initializable} from "@openzeppelin/contracts/proxy/utils/Initializable.sol";
import {ISemver} from "interfaces/universal/ISemver.sol";
import {Types} from "@optimism/src/libraries/Types.sol";
import {Constants} from "@optimism/src/libraries/Constants.sol";
import {ISP1Verifier} from "@sp1-contracts/src/ISP1Verifier.sol";
import {GameType, GameTypes, Claim} from "@optimism/src/dispute/lib/Types.sol";
import {IDisputeGame} from "interfaces/dispute/IDisputeGame.sol";
import {IDisputeGameFactory} from "interfaces/dispute/IDisputeGameFactory.sol";

contract ComposeL2OutputOracle is Initializable, ISemver {
    uint32 public constant COMPOSE_GAME_TYPE = 5555;
    struct InitParams {
        address proposer;
        address owner;
        uint256 finalizationPeriodSeconds;
        uint256 l2BlockTime;
        bytes32 aggregationVkey;
        bytes32 rangeVkeyCommitment;
        bytes32 rollupConfigHash;
        bytes32 startingOutputRoot;
        uint256 startingBlockNumber;
        uint256 startingTimestamp;
        uint256 submissionInterval;
        address verifier;
        address disputeGameFactory;
        uint256 fallbackTimeout;
    }

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

    /// @notice The version of the initializer on the contract. Used for managing upgrades.
    uint8 public constant initializerVersion = 1;

    /// @notice The number of the first L2 block recorded in this contract.
    uint256 public startingBlockNumber;

    /// @notice The timestamp of the first L2 block recorded in this contract.
    uint256 public startingTimestamp;

    /// @notice The interval in L2 blocks at which checkpoints must be submitted.
    uint256 public submissionInterval;

    /// @notice The time between L2 blocks in seconds.
    uint256 public l2BlockTime;

    /// @notice The verification key of the aggregation SP1 program.
    bytes32 public aggregationVkey;

    /// @notice The verification key commitment for the range SP1 program.
    bytes32 public rangeVkeyCommitment;

    /// @notice The rollup config hash.
    bytes32 public rollupConfigHash;

    /// @notice The deployed SP1Verifier contract to verify proofs.
    address public verifier;

    /// @notice The owner of the contract, who has admin permissions.
    address public owner;

    mapping(address => bool) public approvedProposers;

    Types.OutputProposal[] public l2Outputs;

    address public challenger;

    uint256 public finalizationPeriodSeconds;

    address public disputeGameFactory;

    uint256 public fallbackTimeout;

    event L2OutputProposed(
        uint256 indexed superBlockNumber,
        uint256 indexed l2BlockNumber,
        bytes32 indexed outputRoot,
        uint256 l1Timestamp
    );

    event SuperblockOutputProposed(
        uint256 indexed superBlockNumber,
        uint256 indexed l1BlockNumber,
        bytes32 indexed outputRoot,
        uint256 l1Timestamp
    );

    constructor() {
        _disableInitializers();
    }

    /// @notice Initializer.
    /// @param _initParams The initialization parameters for the contract.
    function initialize(
        InitParams memory _initParams
    ) public reinitializer(initializerVersion) {
        require(
            _initParams.startingTimestamp <= block.timestamp,
            "L2OutputOracle: starting L2 timestamp must be less than current time"
        );

        if (l2Outputs.length == 0) {
            l2Outputs.push(
                Types.OutputProposal({
                    outputRoot: _initParams.startingOutputRoot,
                    timestamp: uint128(_initParams.startingTimestamp),
                    l2BlockNumber: uint128(_initParams.startingBlockNumber)
                })
            );

            startingBlockNumber = _initParams.startingBlockNumber;
            startingTimestamp = _initParams.startingTimestamp;
        }

        submissionInterval = _initParams.submissionInterval;
        l2BlockTime = _initParams.l2BlockTime;
        challenger = address(0);
        finalizationPeriodSeconds = _initParams.finalizationPeriodSeconds;

        approvedProposers[_initParams.proposer] = true;

        aggregationVkey = _initParams.aggregationVkey;
        rangeVkeyCommitment = _initParams.rangeVkeyCommitment;
        rollupConfigHash = _initParams.rollupConfigHash;

        verifier = _initParams.verifier;

        owner = _initParams.owner;

        disputeGameFactory = _initParams.disputeGameFactory;
        fallbackTimeout = _initParams.fallbackTimeout;
    }

    /// @notice Accepts an outputRoot and the timestamp of the corresponding L2 block.
    ///         The timestamp must be equal to the current value returned by `nextTimestamp()` in
    ///         order to be accepted. This function may only be called by the Proposer.
    /// @param _outputRoot    The L2 output of the checkpoint block.
    /// @param _l2BlockNumber The L2 block number of the outputRoot.
    /// @param _l1BlockHash   The hash of the L1 block where this proposal was first made.
    /// @param _l1BlockNumber The block number with the specified block hash.
    /// @param extraData      Extra data for the proposal, including proof and preimage.
    /// @dev Security Note: This contract uses `tx.origin` for proposer permission control due to usage of this contract
    ///      in the OPSuccinctDisputeGame, created via DisputeGameFactory using the Clone With Immutable Arguments (CWIA) pattern.
    ///
    ///      In this setup:
    ///      - `msg.sender` is the newly created game contract, not an approved proposer.
    ///      - `tx.origin` is the actual user initiating the transaction.
    ///
    ///      While `tx.origin` can be vulnerable in general, it is safe here because:
    ///      - Only trusted proposers/relayers call this contract.
    ///      - Proposers are expected to interact solely with trusted contracts.
    ///
    ///      As long as proposers avoid untrusted contracts, `tx.origin` is as secure as `msg.sender` in this context.
    function proposeL2Output(
        bytes32 _outputRoot,
        uint256 _l2BlockNumber,
        bytes32 _l1BlockHash,
        uint256 _l1BlockNumber,
        bytes memory extraData
    ) external {
        bytes32 currentL1BlockHash = blockhash(_l1BlockNumber);
        require(currentL1BlockHash == _l1BlockHash, "L2OutputOracle: provided L1 block hash does not match actual L1 block hash");

        require(
            _l2BlockNumber == nextBlockNumber(),
            "L2OutputOracle: block number must be equal to next expected block number"
        );

        require(
            computeL2Timestamp(_l2BlockNumber) < block.timestamp,
            "L2OutputOracle: cannot propose L2 output in the future"
        );

        require(
            _outputRoot != bytes32(0),
            "L2OutputOracle: L2 output proposal cannot be the zero hash"
        );

        if (disputeGameFactory != address(0)) {
            GameType gt = GameType.wrap(COMPOSE_GAME_TYPE);
            Claim rc = Claim.wrap(_outputRoot);
            (IDisputeGame game,) = IDisputeGameFactory(disputeGameFactory).games(gt, rc, extraData);
            require(address(game) == msg.sender, "L2OutputOracle: caller must be the dispute game contract");
        } else {
            bool isFallback = (block.timestamp - l2Outputs[l2Outputs.length - 1].timestamp > fallbackTimeout);
            require(approvedProposers[tx.origin] || isFallback, "L2OutputOracle: only approved proposers or fallback mode can propose new outputs");

            bytes memory superRootPreimage;
            bytes memory aggregatedProof;
            bool asr;
            address sp1Verifier;
            bytes32 aggregationVkeyLocal;
            bytes32 cohortCommitment;

            (superRootPreimage, aggregatedProof, asr, sp1Verifier, aggregationVkeyLocal, cohortCommitment) = abi.decode(extraData, (bytes, bytes, bool, address, bytes32, bytes32));

            ISP1Verifier(sp1Verifier).verifyProof(
                aggregationVkeyLocal,
                abi.encode(_outputRoot, cohortCommitment, _l1BlockHash),
                aggregatedProof
            );
        }

        // Decode the preimage to get the outputs
        SuperblockAggregationOutputs memory superBlockAggOutputs = abi.decode(abi.decode(extraData, (bytes)), (SuperblockAggregationOutputs));

        l2Outputs.push(
            Types.OutputProposal({
                outputRoot: _outputRoot,
                timestamp: uint128(block.timestamp),
                l2BlockNumber: uint128(_l2BlockNumber)
            })
        );

        BootInfoStruct memory bootInfo;

        for (uint256 i = 0; i < superBlockAggOutputs.bootInfo.length; i++) {
            bootInfo = superBlockAggOutputs.bootInfo[i];

            emit L2OutputProposed(
                superBlockAggOutputs.superblockNumber,
                bootInfo.l2BlockNumber,
                bootInfo.l2PostRoot,
                block.timestamp
            );
        }

        emit SuperblockOutputProposed(
            superBlockAggOutputs.superblockNumber,
            _l1BlockNumber,
            _outputRoot,
            block.timestamp
        );
    }

    /// @notice Returns the block number of the next L2 block that needs to be checkpointed.
    function nextBlockNumber() public view returns (uint256) {
        return startingBlockNumber + l2Outputs.length * submissionInterval;
    }

    /// @notice Computes the timestamp of the L2 block number.
    function computeL2Timestamp(uint256 _l2BlockNumber) public view returns (uint256) {
        require(_l2BlockNumber >= startingBlockNumber, "L2OutputOracle: block number must be greater than or equal to starting block number");
        return startingTimestamp + (_l2BlockNumber - startingBlockNumber) * l2BlockTime / submissionInterval;
    }

    function version() external pure returns (string memory) {
        return "0.0.1";
    }
}