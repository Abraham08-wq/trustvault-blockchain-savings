// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {ChamaVault} from "../src/ChamaVault.sol";

contract ChamaVaultTest is Test {
    ChamaVault vault;
    address treasurer = address(0xA1);
    address chair = address(0xA2);
    address secretary = address(0xA3);
    address member4 = address(0xA4);
    address outsider = address(0xDEAD);

    function setUp() public {
        address[] memory m = new address[](4);
        m[0] = treasurer;
        m[1] = chair;
        m[2] = secretary;
        m[3] = member4;
        vault = new ChamaVault(m, 2); // 2 approvals needed besides the proposer
    }

    function _propose() internal returns (uint256) {
        vm.prank(treasurer);
        return vault.propose(5_000_000, keccak256("2547XXXXXXXX"), keccak256("school fees loan"));
    }

    function test_TreasurerCannotWithdrawAlone() public {
        uint256 id = _propose();
        vm.prank(treasurer);
        vm.expectRevert(ChamaVault.ProposerCannotApprove.selector);
        vault.approve(id);

        vm.prank(treasurer);
        vm.expectRevert(abi.encodeWithSelector(ChamaVault.NotEnoughApprovals.selector, uint32(0), uint32(2)));
        vault.execute(id);
    }

    function test_ExecutesAfterThreshold() public {
        uint256 id = _propose();
        vm.prank(chair);
        vault.approve(id);
        vm.prank(secretary);
        vault.approve(id);
        vm.prank(member4);
        vault.execute(id);
        (,,,,, bool executed,) = vault.proposals(id);
        assertTrue(executed);
    }

    function test_NoDoubleApproval() public {
        uint256 id = _propose();
        vm.startPrank(chair);
        vault.approve(id);
        vm.expectRevert(ChamaVault.AlreadyApproved.selector);
        vault.approve(id);
        vm.stopPrank();
    }

    function test_OutsiderBlocked() public {
        vm.prank(outsider);
        vm.expectRevert(ChamaVault.NotMember.selector);
        vault.propose(1, bytes32(0), bytes32(0));
    }

    function test_CannotExecuteTwice() public {
        uint256 id = _propose();
        vm.prank(chair);
        vault.approve(id);
        vm.prank(secretary);
        vault.approve(id);
        vm.prank(chair);
        vault.execute(id);
        vm.prank(chair);
        vm.expectRevert(ChamaVault.AlreadyFinalised.selector);
        vault.execute(id);
    }

    function test_InvalidThresholdRejected() public {
        address[] memory m = new address[](2);
        m[0] = treasurer;
        m[1] = chair;
        vm.expectRevert(ChamaVault.InvalidThreshold.selector);
        new ChamaVault(m, 2);
    }
}
