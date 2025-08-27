// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

interface IToken {
    function burn(address account, uint256 value) external;
    function mint(address account, uint256 value) external;
}
