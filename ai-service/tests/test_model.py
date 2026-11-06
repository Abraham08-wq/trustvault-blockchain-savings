from app.model import FraudScorer, WithdrawalFeatures

scorer = FraudScorer()


def normal_request(**overrides):
    base = dict(
        group_id="g1",
        amount=5_000,
        group_balance=60_000,
        requester_role="member",
        requester_tenure_days=400,
        requester_total_contributed=12_000,
        group_avg_withdrawal=5_500,
        withdrawals_last_7_days=0,
        recipient_is_requester=True,
        recipient_is_member=True,
        hour_of_day=14,
    )
    base.update(overrides)
    return WithdrawalFeatures(**base)


def test_normal_member_loan_is_low_risk():
    s = scorer.score(normal_request())
    assert s.level == "low", s


def test_treasurer_emptying_account_to_self_is_high_risk():
    s = scorer.score(
        normal_request(
            amount=58_000,
            requester_role="treasurer",
            recipient_is_requester=True,
            hour_of_day=2,
        )
    )
    assert s.level == "high", s
    assert any("80%" in r for r in s.reasons)
    assert any("themselves" in r for r in s.reasons)


def test_outside_recipient_and_new_member_raises_risk():
    s = scorer.score(normal_request(recipient_is_member=False, recipient_is_requester=False, requester_tenure_days=5, amount=20_000))
    assert s.level in ("medium", "high"), s


def test_scores_are_bounded():
    s = scorer.score(normal_request(amount=10_000_000, group_balance=1, withdrawals_last_7_days=50))
    assert 0.0 <= s.score <= 1.0
