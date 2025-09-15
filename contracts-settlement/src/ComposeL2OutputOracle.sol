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
    struct InitParams {
        address proposer;
        address owner;
        // uint256 finalizationPeriodSeconds;
        // uint256 l2BlockTime;
        bytes32 aggregationVkey;
        bytes32 rangeVkeyCommitment;
        bytes32 rollupConfigHash;
        bytes32 startingOutputRoot;
        uint256 startingBlockNumber;
        uint256 startingTimestamp;
        // uint256 submissionInterval;
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

    /// @notice The verification key of the aggregation SP1 program.
    bytes32 aggregationVkey;

    /// @notice The deployed SP1Verifier contract to verify proofs.
    address public verifier;

    /// @notice The owner of the contract, who has admin permissions.
    address public owner;

    mapping(address => bool) public approvedProposers;

    Types.OutputProposal[] public l2Outputs;

    address public challenger;

    uint256 public finalizationPeriodSeconds;

    bool internal _enteredDGFCreate;

    address public disputeGameFactory;

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

        // For proof verification to work, there must be an initial output.
        // Disregard the _startingBlockNumber and _startingTimestamp parameters during upgrades, as they're already set.
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

        challenger = address(0);
        finalizationPeriodSeconds = 0;

        // Add the initial proposer.
        approvedProposers[_initParams.proposer] = true;

        aggregationVkey = _initParams.aggregationVkey;

        verifier = _initParams.verifier;

        owner = _initParams.owner;

        _enteredDGFCreate = true;

        disputeGameFactory = _initParams.disputeGameFactory;
    }

    /// @notice Accepts an outputRoot and the timestamp of the corresponding L2 block.
    ///         The timestamp must be equal to the current value returned by `nextTimestamp()` in
    ///         order to be accepted. This function may only be called by the Proposer.
    /// @param _outputRoot    The L2 output of the checkpoint block.
    /// @param _l1BlockNumber The block number with the specified block hash.
    /// @dev Modified the function signature to exclude the `_l1BlockHash` parameter, as it's redundant
    ///      for OP Succinct given the `_l1BlockNumber` parameter.
    /// @dev Security Note: This contract uses `tx.origin` for proposer permission control due to usage of this contract
    ///      in the OPSuccinctDisputeGame, created via DisputeGameFactory using the Clone With Immutable Arguments (CWIA) pattern.
    ///
    ///      In this setup:
    ///      - `msg.sender` is the newly created game contract, not an approved proposer.
    ///      - `tx.origin` identifies the actual user initiating the transaction.
    ///
    ///      While `tx.origin` can be vulnerable in general, it is safe here because:
    ///      - Only trusted proposers/relayers call this contract.
    ///      - Proposers are expected to interact solely with trusted contracts.
    ///
    ///      As long as proposers avoid untrusted contracts, `tx.origin` is as secure as `msg.sender` in this context.
    function proposeL2Output(
        bytes32 _outputRoot,
        uint256 _l1BlockNumber,
        bytes memory extraData
    ) external {
        // The proposer must be explicitly approved
        // or the fallback timeout has been exceeded allowing anyone to propose.
        require(
            approvedProposers[tx.origin],
            // || (block.timestamp - lastProposalTimestamp() > fallbackTimeout), TODO fallback implementation for permissionless proposing?
            "L2OutputOracle: only approved proposers can propose new outputs"
        );

        // TODO check
        /**
        require(
            _l2BlockNumber >= nextBlockNumber(),
            "L2OutputOracle: block number must be greater than or equal to next expected block number"
        );


        require(
            computeL2Timestamp(_l2BlockNumber) < block.timestamp,
            "L2OutputOracle: cannot propose L2 output in the future"
        );
        */

        // If the dispute game factory is set, make sure that we are calling this function from within
        // DisputeGameFactory.create.
        if (disputeGameFactory != address(0)) {
            require(
                _enteredDGFCreate,
                "L2OutputOracle: cannot propose L2 output from outside DisputeGameFactory.create while disputeGameFactory is set"
            );
        } else {
            require(
                !_enteredDGFCreate,
                "L2OutputOracle: cannot propose L2 output from inside DisputeGameFactory.create without setting disputeGameFactory"
            );
        }

        require(
            _outputRoot != bytes32(0),
            "L2OutputOracle: L2 output proposal cannot be the zero hash"
        );

        /** TODO I think its safe to remove it
        OpSuccinctConfig memory config = opSuccinctConfigs;
        require(isValidOpSuccinctConfig(config), "L2OutputOracle: invalid OP Succinct configuration");
        **/

        /** TODO Check if its safe to remove it
        bytes32 l1BlockHash = historicBlockHashes[_l1BlockNumber];
        if (l1BlockHash == bytes32(0)) {
            revert L1BlockHashNotCheckpointed();
        }
        **/

        // Decode the struct and save to storage for getter
        (
            SuperblockAggregationOutputs memory superBlockAggOutputs,
            bytes memory proof
        ) = abi.decode(extraData, (SuperblockAggregationOutputs, bytes));

        ISP1Verifier(verifier).verifyProof(
            aggregationVkey,
            abi.encode(superBlockAggOutputs),
            proof
        );

        BootInfoStruct memory bootInfo;

        for (uint256 i = 0; i < superBlockAggOutputs.bootInfo.length; i++) {
            bootInfo = superBlockAggOutputs.bootInfo[i];

            // TODO I think we need a way to identify the rollup chain, maybe we can use the rollupConfigHash?
            emit L2OutputProposed(
                superBlockAggOutputs.superblockNumber,
                bootInfo.l2BlockNumber,
                bootInfo.l2PostRoot,
                block.timestamp
            );
        }

        emit SuperblockOutputProposed(
            superBlockAggOutputs.superblockNumber,
            block.number,
            superBlockAggOutputs.parentSuperblockBatchHash,
            block.timestamp
        );
    }

    function version() external pure returns (string memory) {
        return "0.0.1";
    }
}
