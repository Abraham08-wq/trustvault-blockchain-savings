"""Fraud scoring for withdrawal requests.

Two layers, combined:
1. Rules that encode known chama fraud patterns (always explainable).
2. An IsolationForest anomaly model that flags requests unlike normal group behaviour.

The anomaly model starts from synthetic "normal" data so it works on day one, and should be
retrained on real (consented, anonymised) group data once pilots run.
"""

from __future__ import annotations

from dataclasses import dataclass, field

import numpy as np
from sklearn.ensemble import IsolationForest

FEATURES = [
    "share_of_balance",      # amount / group balance
    "amount_vs_avg",         # amount / group's average withdrawal
    "is_official",           # requester is chair or treasurer
    "self_payment",          # recipient phone == requester phone
    "outside_recipient",     # recipient is not a group member
    "withdrawals_7d",        # withdrawals in the last 7 days
    "tenure_days_log",       # log(1 + membership age in days)
    "contrib_ratio",         # amount / what requester has contributed
    "odd_hour",              # between 23:00 and 05:00
]


@dataclass
class WithdrawalFeatures:
    group_id: str
    amount: float
    group_balance: float
    requester_role: str = "member"
    requester_tenure_days: float = 0.0
    requester_total_contributed: float = 0.0
    group_avg_withdrawal: float = 0.0
    withdrawals_last_7_days: int = 0
    recipient_is_requester: bool = False
    recipient_is_member: bool = True
    hour_of_day: int = 12

    def vector(self) -> np.ndarray:
        balance = max(self.group_balance, 1.0)
        avg = self.group_avg_withdrawal if self.group_avg_withdrawal > 0 else self.amount
        contributed = max(self.requester_total_contributed, 1.0)
        return np.array(
            [
                min(self.amount / balance, 2.0),
                min(self.amount / max(avg, 1.0), 10.0),
                1.0 if self.requester_role in ("chair", "treasurer") else 0.0,
                1.0 if self.recipient_is_requester else 0.0,
                0.0 if self.recipient_is_member else 1.0,
                float(min(self.withdrawals_last_7_days, 20)),
                float(np.log1p(max(self.requester_tenure_days, 0.0))),
                min(self.amount / contributed, 50.0),
                1.0 if (self.hour_of_day < 5 or self.hour_of_day >= 23) else 0.0,
            ],
            dtype=float,
        )


@dataclass
class Score:
    score: float
    level: str
    reasons: list[str] = field(default_factory=list)


def _synthetic_normal(n: int = 4000, seed: int = 7) -> np.ndarray:
    """Typical, healthy chama withdrawals: modest shares, members paid, daytime, low frequency."""
    rng = np.random.default_rng(seed)
    share = rng.beta(2, 9, n)                      # mostly 5-30% of balance
    vs_avg = rng.lognormal(0.0, 0.35, n)           # near the group's usual size
    official = rng.binomial(1, 0.3, n)
    self_pay = rng.binomial(1, 0.15, n) * (1 - official)  # members borrowing for themselves
    outside = rng.binomial(1, 0.03, n)
    w7 = rng.poisson(0.6, n)
    tenure = np.log1p(rng.uniform(60, 1500, n))
    contrib = rng.lognormal(-0.5, 0.6, n)
    odd = rng.binomial(1, 0.02, n)
    return np.column_stack([share, vs_avg, official, self_pay, outside, w7, tenure, contrib, odd])


class FraudScorer:
    def __init__(self, seed: int = 7) -> None:
        self.model = IsolationForest(n_estimators=200, contamination=0.02, random_state=seed)
        normal = _synthetic_normal(seed=seed)
        self.model.fit(normal)
        # Calibrate anomaly scores on the normal data so 0 = typical, 1 = extremely unusual.
        raw = -self.model.score_samples(normal)
        self._lo = float(np.percentile(raw, 50))
        self._hi = float(np.percentile(raw, 99.9))

    def anomaly(self, x: np.ndarray) -> float:
        raw = float(-self.model.score_samples(x.reshape(1, -1))[0])
        span = max(self._hi - self._lo, 1e-9)
        return float(np.clip((raw - self._lo) / span, 0.0, 1.0))

    def score(self, f: WithdrawalFeatures) -> Score:
        x = f.vector()
        rule_score, reasons = rules(f, x)
        anomaly = self.anomaly(x)
        if anomaly >= 0.6:
            reasons.append("unusual compared with normal group behaviour (AI anomaly model)")
        combined = float(np.clip(0.65 * rule_score + 0.35 * anomaly, 0.0, 1.0))
        # Strong single signals should never be averaged away.
        combined = max(combined, rule_score if rule_score >= 0.6 else 0.0)
        level = "high" if combined >= 0.6 else "medium" if combined >= 0.3 else "low"
        return Score(score=round(combined, 3), level=level, reasons=reasons)


def rules(f: WithdrawalFeatures, x: np.ndarray) -> tuple[float, list[str]]:
    share, vs_avg, official, self_pay, outside, w7, tenure_log, contrib_ratio, odd = x
    s = 0.0
    reasons: list[str] = []
    if share >= 0.8:
        s += 0.5
        reasons.append("withdrawal is 80% or more of the group balance")
    elif share >= 0.5:
        s += 0.3
        reasons.append("withdrawal is half or more of the group balance")
    if official and self_pay:
        s += 0.3
        reasons.append("an official is paying group money to themselves")
    if outside:
        s += 0.2
        reasons.append("recipient is not a group member")
    if f.group_avg_withdrawal > 0 and vs_avg >= 4:
        s += 0.15
        reasons.append("much larger than this group's usual withdrawals")
    if w7 >= 3:
        s += 0.2
        reasons.append("many withdrawals in the last 7 days")
    if f.requester_tenure_days < 30:
        s += 0.15
        reasons.append("requester joined less than 30 days ago")
    if contrib_ratio >= 10 and not official:
        s += 0.1
        reasons.append("amount is far above what the requester has contributed")
    if odd:
        s += 0.1
        reasons.append("requested at an unusual hour")
    return min(s, 1.0), reasons
