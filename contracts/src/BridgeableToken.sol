// SPDX-License-Identifier: GPL-3
pragma solidity 0.8.30;

import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import { IBridgeableToken } from "@ssv/src/interfaces/IBridgeableToken.sol";

/**
 * @title BridgeableToken
 * @notice An example bridgeable token fro cross-chain operations
 *
 * @author
 * SSV Labs
 */
contract BridgeableToken is ERC20, IBridgeableToken {

    /// @notice The address of the bridge contract authorized to mint and burn tokens.
    address public immutable BRIDGE;

    /// @dev Restricts function access to the bridge contract.
    modifier onlyBridge() {
        if (msg.sender != BRIDGE) {
            revert Unauthorized();
        }
        _;
    }

    /// @notice Initializes the token with name, symbol, and bridge address.
    /// @param bridge Address of the bridge contract.
    constructor(address bridge) ERC20("BridgeableTokenExample", "BTK") {
        BRIDGE = bridge;
    }

    /// @notice Burns tokens from an account, callable only by the bridge.
    /// @param account Address from which tokens are burned.
    /// @param value Amount of tokens to burn.
    function burn(address account, uint256 value) public onlyBridge {
        _burn(account, value);
    }

    /// @notice Mints tokens to an account, callable only by the bridge.
    /// @param account Address to which tokens are minted.
    /// @param value Amount of tokens to mint.
    function mint(address account, uint256 value) public onlyBridge {
        _mint(account, value);
    }
}
