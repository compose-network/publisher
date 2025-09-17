// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {Test} from "forge-std/Test.sol";
import {ComposeL2OutputOracle} from "../src/ComposeL2OutputOracle.sol";
import {Proxy} from "@optimism/src/universal/Proxy.sol";

contract Utils is Test {
    function deployL2OutputOracle(ComposeL2OutputOracle.InitParams memory initParams)
    public
    returns (ComposeL2OutputOracle)
    {
        bytes memory initializationParams =
                            abi.encodeWithSelector(ComposeL2OutputOracle.initialize.selector, initParams);

        Proxy l2OutputOracleProxy = new Proxy(address(this));
        l2OutputOracleProxy.upgradeToAndCall(address(new ComposeL2OutputOracle()), initializationParams);

        return ComposeL2OutputOracle(address(l2OutputOracleProxy));
    }
}