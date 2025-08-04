// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
// import { ERC20Bridgeable } from "@openzeppelin/contracts/token/ERC20/extensions/draft-ERC20Bridgeable.sol";

contract MyToken is ERC20 {
    address internal constant SUPERCHAIN_TOKEN_BRIDGE =
        0x4200000000000000000000000000000000000028;
    error Unauthorized();

    constructor() ERC20("MyToken", "MTK") {}

    /**
     * @dev Checks if the caller is the predeployed SuperchainTokenBridge. Reverts otherwise.
     *
     * IMPORTANT: The predeployed SuperchainTokenBridge is only available on chains in the Superchain.
     */
    // function _checkTokenBridge(address caller) internal pure override {
    //    if (caller != SUPERCHAIN_TOKEN_BRIDGE) revert Unauthorized();
    // }
}
