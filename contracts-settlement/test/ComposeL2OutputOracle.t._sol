// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {Test} from "forge-std/Test.sol";
import {ComposeL2OutputOracle} from "../src/ComposeL2OutputOracle.sol";
import {Proxy} from "@optimism/src/universal/Proxy.sol";

import {Utils} from "./Utils.sol";
import {MockVerifier} from "./mock/MockVerifier.sol";

contract ComposeL2OutputOracleUnitTest is Test, Utils {
    ComposeL2OutputOracle private l2oo;
    MockVerifier private verifier;

    address private constant APPROVED_PROPOSER = address(0x1234);
    address private constant NON_APPROVED_PROPOSER = address(0x5678);
    address private constant CHALLENGER = address(0x9ABC);
    address private constant OWNER = address(0xDEF0);

    bytes32 private constant AGGREGATION_VKEY = keccak256("aggregation_vkey");
    bytes32 private constant RANGE_VKEY_COMMITMENT = keccak256("range_vkey");
    bytes32 private constant ROLLUP_CONFIG_HASH = keccak256("rollup_config");
    bytes32 private constant STARTING_OUTPUT_ROOT = keccak256("starting_output");

    uint256 private constant SUBMISSION_INTERVAL = 10;
    uint256 private constant L2_BLOCK_TIME = 2;
    uint256 private constant STARTING_BLOCK_NUMBER = 1000;
    uint256 private constant FINALIZATION_PERIOD = 7 days;
    uint256 private constant FALLBACK_TIMEOUT = 2 days;

    bytes32 private constant GENESIS_CONFIG_NAME = bytes32(0);

    bytes private constant PROOF = hex"a4594c59162bdead7c5ac7b05e2b0576eddf5cac6ad631b71129796bfb5db2da2d14189822763aff12bbf03cad631c20a6d4c3c1eaaf9808216d174a2be0af8a996c00e5084af057ddac9445681ec7844e1b52e33a1ed84b5e8106599554107f50e3954518586d4071b44f7c22f0d954bf259a8bf0610ebb4debd43e8eb1dfb29960c9aa0d935506c2b1d79d007a576dc095325189300c0ea459a4d994854cbe82829bac16fda18d408ad0aa80ed7d0e9ccc5af167b4310b4c1430da73640e8e81daeabe0c7a4e8c3ca2b20393882c62c5815a5703f990b7166809942de5d7dfabbc4fed013ad62d57aaefbf0a600025cc420b9195936eb202a9da25acdf27f948840913";
    address private constant PROVER_ADDRESS = address(0x7890);

    bytes32 private constant OUTPUT_ROOT = keccak256("output_root");
    bytes32 private constant L1_HASH = keccak256("l1_hash");
    bytes32 private constant PARENT_SUPERBLOCK_BATCH_HASH = bytes32(0x66cec985afe7e41f97a2f77c876fe9015be47f18baa0bd87c59795c52887df19);
    address private constant NEW_VERIFIER = address(0xABCD);
    bytes32 private constant NEW_VKEY = keccak256("new_vkey");

    ComposeL2OutputOracle.InitParams internal initParams = ComposeL2OutputOracle.InitParams(
        APPROVED_PROPOSER,
        OWNER,
        AGGREGATION_VKEY,
        STARTING_BLOCK_NUMBER,
        address(verifier)
    );

    function setUp() public {
        address verifierAddress = address(new MockVerifier());
        initParams.verifier = verifierAddress;

        l2oo = deployL2OutputOracle(initParams);

        vm.warp(block.timestamp + 1000);
    }

    function test_initializer_configuresEverythingCorrectly() public {
        assertEq(l2oo.superBlockNumber(), STARTING_BLOCK_NUMBER);
        assertEq(l2oo.aggregationVkey(), AGGREGATION_VKEY);
        assertEq(l2oo.verifier(), initParams.verifier);
        assertEq(l2oo.owner(), OWNER);
        assertEq(l2oo.approvedProposer(), APPROVED_PROPOSER);
        assertEq(l2oo.version(), "0.0.1");
    }

    function test_setAggregationVkey_byOwner() public {
        vm.prank(OWNER);
        l2oo.setAggregationVkey(NEW_VKEY);

        assertEq(l2oo.aggregationVkey(), NEW_VKEY);
    }

    function test_setAggregationVkey_reverts_whenNotOwner() public {
        vm.expectRevert("ComposeL2OutputOracle: only owner can update aggregation vkey");
        vm.prank(NON_APPROVED_PROPOSER);
        l2oo.setAggregationVkey(NEW_VKEY);
    }

    function test_setVerifier_byOwner() public {
        vm.prank(OWNER);
        l2oo.setVerifier(NEW_VERIFIER);

        assertEq(l2oo.verifier(), NEW_VERIFIER);
    }

    function test_setVerifier_reverts_whenNotOwner() public {
        vm.expectRevert("ComposeL2OutputOracle: only owner can update verifier");
        vm.prank(NON_APPROVED_PROPOSER);
        l2oo.setVerifier(NEW_VERIFIER);
    }

    function test_proposeL2Output_byApprovedProposer() public {
        ComposeL2OutputOracle.SuperblockAggregationOutputs memory superBlockAggOutputs = ComposeL2OutputOracle.SuperblockAggregationOutputs({
            superblockNumber: STARTING_BLOCK_NUMBER + 1,
            parentSuperblockBatchHash: PARENT_SUPERBLOCK_BATCH_HASH,
            bootInfo: new ComposeL2OutputOracle.BootInfoStruct[](0)
        });

        bytes memory extraData = abi.encode(superBlockAggOutputs, PROOF);

        MockVerifier mockVerifier = MockVerifier(initParams.verifier);
        mockVerifier.mockVerifyProof(true);

        vm.prank(APPROVED_PROPOSER, APPROVED_PROPOSER);
        l2oo.proposeL2Output(OUTPUT_ROOT, L1_HASH, extraData);

        assertEq(l2oo.superBlockNumber(), STARTING_BLOCK_NUMBER + 1);
    }

    function test_proposeL2Output_reverts_notApprovedProposer() public {
        ComposeL2OutputOracle.SuperblockAggregationOutputs memory superBlockAggOutputs = ComposeL2OutputOracle.SuperblockAggregationOutputs({
            superblockNumber: STARTING_BLOCK_NUMBER + 1,
            parentSuperblockBatchHash: PARENT_SUPERBLOCK_BATCH_HASH,
            bootInfo: new ComposeL2OutputOracle.BootInfoStruct[](0)
        });

        bytes memory extraData = abi.encode(superBlockAggOutputs, PROOF);

        vm.expectRevert("L2OutputOracle: only approved proposers can propose new outputs");
        vm.prank(NON_APPROVED_PROPOSER, NON_APPROVED_PROPOSER);
        l2oo.proposeL2Output(OUTPUT_ROOT, L1_HASH, extraData);
    }

    function test_proposeL2Output_reverts_zeroOutputRoot() public {
        ComposeL2OutputOracle.SuperblockAggregationOutputs memory superBlockAggOutputs = ComposeL2OutputOracle.SuperblockAggregationOutputs({
            superblockNumber: STARTING_BLOCK_NUMBER + 1,
            parentSuperblockBatchHash: PARENT_SUPERBLOCK_BATCH_HASH,
            bootInfo: new ComposeL2OutputOracle.BootInfoStruct[](0)
        });

        bytes memory extraData = abi.encode(superBlockAggOutputs, PROOF);

        MockVerifier mockVerifier = MockVerifier(initParams.verifier);
        mockVerifier.mockVerifyProof(true);

        vm.expectRevert("ComposeL2OutputOracle: L2 output proposal cannot be the zero hash");
        vm.prank(APPROVED_PROPOSER, APPROVED_PROPOSER);
        l2oo.proposeL2Output(bytes32(0), L1_HASH, extraData);
    }

    function test_proposeL2Output_reverts_proofVerificationFails() public {
        ComposeL2OutputOracle.SuperblockAggregationOutputs memory superBlockAggOutputs = ComposeL2OutputOracle.SuperblockAggregationOutputs({
            superblockNumber: STARTING_BLOCK_NUMBER + 1,
            parentSuperblockBatchHash: PARENT_SUPERBLOCK_BATCH_HASH,
            bootInfo: new ComposeL2OutputOracle.BootInfoStruct[](0)
        });

        bytes memory extraData = abi.encode(superBlockAggOutputs, PROOF);

        MockVerifier mockVerifier = MockVerifier(initParams.verifier);
        mockVerifier.mockVerifyProof(false);

        vm.expectRevert("MockVerifier: Proof verification failed");
        vm.prank(APPROVED_PROPOSER, APPROVED_PROPOSER);
        l2oo.proposeL2Output(OUTPUT_ROOT, L1_HASH, extraData);
    }

    function test_version_returnsCorrectVersion() public {
        assertEq(l2oo.version(), "0.0.1");
    }
}