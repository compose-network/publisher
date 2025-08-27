// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
// import { ERC20Bridgeable } from "@openzeppelin/contracts/token/ERC20/extensions/draft-ERC20Bridgeable.sol";
import { IToken } from "@ssv/src/interfaces/IToken.sol";

contract MyToken is ERC20, IToken {
    address internal constant TOKEN_BRIDGE =
        0x4200000000000000000000000000000000000028;

    error Unauthorized();

    constructor() ERC20("MyToken", "MTK") {}

    /**
     * @dev Checks if the caller is the pre-deployed SuperchainTokenBridge. Reverts otherwise.
     *
     * IMPORTANT: The pre-deployed SuperchainTokenBridge is only available on chains in the Superchain.
     */
    // function _checkTokenBridge(address caller) internal pure override {
    //    if (caller != SUPERCHAIN_TOKEN_BRIDGE) revert Unauthorized();
    // }

    function burn(address account, uint256 value) public {
        // _checkTokenBridge(account);
        _burn(account, value);
    }

    function mint(address account, uint256 value) public {
        // _checkTokenBridge(account);
        _mint(account, value);
    }
}
