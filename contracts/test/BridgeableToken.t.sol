// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

import { Setup } from "@ssv/test/Setup.t.sol";

contract TokenTest is Setup {
    function testTokenMint(uint256 mintedBalance) public {
        uint256 balance = myToken.balanceOf(DEPLOYER);
        assertEq(balance, 0, "Initial balance should be 0");

        vm.prank(address(bridge));
        myToken.mint(DEPLOYER, mintedBalance);
        balance = myToken.balanceOf(DEPLOYER);
        assertEq(
            balance,
            mintedBalance,
            "Final balance should be the mintedBalance"
        );
    }

    function testTokenBurn(uint256 burnedBalance) public {
        uint256 mintedBalance = 100;
        vm.assume(burnedBalance <= mintedBalance);
        testTokenMint(mintedBalance);
        uint256 balance = myToken.balanceOf(DEPLOYER);
        assertEq(
            balance,
            mintedBalance,
            "Initial balance should be the minted one"
        );

        vm.prank(address(bridge));
        myToken.burn(DEPLOYER, burnedBalance);
        balance = myToken.balanceOf(DEPLOYER);
        assertEq(
            balance,
            mintedBalance - burnedBalance,
            "Final balance should be the difference"
        );
    }
}
