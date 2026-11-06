// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title ChamaVault
/// @notice On-chain M-of-N approval rules for one savings group (phase 2: members sign from their own phones).
/// The vault holds NO funds. It records withdrawal proposals and approvals; the backend only pays out
/// through mobile money after this contract marks a proposal as executed.
contract ChamaVault {
    struct Proposal {
        address proposer;
        uint256 amountCents; // amount in local currency cents (e.g. KES cents)
        bytes32 recipientRef; // hash of recipient phone number (no personal data on-chain)
        bytes32 reasonHash; // hash of the stated reason
        uint32 approvals;
        bool executed;
        bool cancelled;
    }

    address[] public members;
    mapping(address => bool) public isMember;
    uint32 public immutable threshold;

    Proposal[] public proposals;
    mapping(uint256 => mapping(address => bool)) public approvedBy;

    event Proposed(uint256 indexed id, address indexed proposer, uint256 amountCents, bytes32 recipientRef);
    event Approved(uint256 indexed id, address indexed member, uint32 approvals);
    event Executed(uint256 indexed id);
    event Cancelled(uint256 indexed id);

    error NotMember();
    error InvalidThreshold();
    error DuplicateMember();
    error ZeroAddress();
    error UnknownProposal();
    error AlreadyApproved();
    error ProposerCannotApprove();
    error NotEnoughApprovals(uint32 have, uint32 need);
    error AlreadyFinalised();
    error ZeroAmount();

    modifier onlyMember() {
        if (!isMember[msg.sender]) revert NotMember();
        _;
    }

    /// @param _members group members' addresses
    /// @param _threshold approvals required, NOT counting the proposer (proposer can never approve own request)
    constructor(address[] memory _members, uint32 _threshold) {
        if (_members.length < 2 || _threshold == 0 || _threshold > _members.length - 1) revert InvalidThreshold();
        for (uint256 i = 0; i < _members.length; i++) {
            address m = _members[i];
            if (m == address(0)) revert ZeroAddress();
            if (isMember[m]) revert DuplicateMember();
            isMember[m] = true;
            members.push(m);
        }
        threshold = _threshold;
    }

    function propose(uint256 amountCents, bytes32 recipientRef, bytes32 reasonHash)
        external
        onlyMember
        returns (uint256 id)
    {
        if (amountCents == 0) revert ZeroAmount();
        id = proposals.length;
        proposals.push(Proposal(msg.sender, amountCents, recipientRef, reasonHash, 0, false, false));
        emit Proposed(id, msg.sender, amountCents, recipientRef);
    }

    function approve(uint256 id) external onlyMember {
        Proposal storage p = _get(id);
        if (p.executed || p.cancelled) revert AlreadyFinalised();
        if (p.proposer == msg.sender) revert ProposerCannotApprove();
        if (approvedBy[id][msg.sender]) revert AlreadyApproved();
        approvedBy[id][msg.sender] = true;
        p.approvals += 1;
        emit Approved(id, msg.sender, p.approvals);
    }

    function execute(uint256 id) external onlyMember {
        Proposal storage p = _get(id);
        if (p.executed || p.cancelled) revert AlreadyFinalised();
        if (p.approvals < threshold) revert NotEnoughApprovals(p.approvals, threshold);
        p.executed = true;
        emit Executed(id);
    }

    function cancel(uint256 id) external {
        Proposal storage p = _get(id);
        if (msg.sender != p.proposer) revert NotMember();
        if (p.executed || p.cancelled) revert AlreadyFinalised();
        p.cancelled = true;
        emit Cancelled(id);
    }

    function memberCount() external view returns (uint256) {
        return members.length;
    }

    function proposalCount() external view returns (uint256) {
        return proposals.length;
    }

    function _get(uint256 id) internal view returns (Proposal storage) {
        if (id >= proposals.length) revert UnknownProposal();
        return proposals[id];
    }
}
