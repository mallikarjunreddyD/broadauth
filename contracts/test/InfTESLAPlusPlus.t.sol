// SPDX-License-Identifier: MIT
pragma solidity ^0.8.12;

import {Test} from "forge-std/Test.sol";
import {InfTESLAplusplus} from "../src/InfTESLAPlusPlus.sol";

contract InfTESLAPlusPlusTest is Test {
    InfTESLAplusplus public sc;
    address public owner = address(0xA11CE);
    uint public constant RCD_ID = 1;

    function setUp() public {
        sc = new InfTESLAplusplus();
        sc.regOwner(owner, RCD_ID);
    }

    function test_StoreAndGetKey() public {
        vm.prank(owner);
        sc.storeKey(RCD_ID, 1, "key0", 100, 200, 2);

        (string memory key, uint startTime, uint endTime, uint delay) = sc
            .getKey(RCD_ID);
        assertEq(key, "key0");
        assertEq(startTime, 100);
        assertEq(endTime, 200);
        assertEq(delay, 2);
    }

    function test_StoreAndGetAdaptiveKey() public {
        vm.prank(owner);
        sc.storeAdaptiveKey(RCD_ID, 1, "key0", 100, 200, 2, 1000, 8000);

        (
            string memory key,
            uint startTime,
            uint endTime,
            uint delay,
            uint tMin,
            uint tMax
        ) = sc.getAdaptiveKey(RCD_ID);
        assertEq(key, "key0");
        assertEq(startTime, 100);
        assertEq(endTime, 200);
        assertEq(delay, 2);
        assertEq(tMin, 1000);
        assertEq(tMax, 8000);
    }

    function test_AdaptiveKeyEnforcesSameChecksAsStoreKey() public {
        // Not the owner: should revert exactly like storeKey does.
        vm.prank(address(0xBAD));
        vm.expectRevert("Only owner of the RCD can call");
        sc.storeAdaptiveKey(RCD_ID, 1, "key0", 100, 200, 2, 1000, 8000);
    }

    function test_GetKeyStillWorksAfterAdaptiveStore() public {
        // storeAdaptiveKey must remain backward compatible with plain getKey.
        vm.prank(owner);
        sc.storeAdaptiveKey(RCD_ID, 1, "key0", 100, 200, 2, 1000, 8000);

        (string memory key, uint startTime, uint endTime, uint delay) = sc
            .getKey(RCD_ID);
        assertEq(key, "key0");
        assertEq(startTime, 100);
        assertEq(endTime, 200);
        assertEq(delay, 2);
    }
}
