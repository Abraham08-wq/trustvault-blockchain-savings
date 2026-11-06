"""ChamaChain AI service: scores withdrawal requests for fraud risk."""

from fastapi import FastAPI
from pydantic import BaseModel, Field

from .model import FraudScorer, WithdrawalFeatures

app = FastAPI(title="ChamaChain AI", version="0.1.0")
scorer = FraudScorer()


class ScoreRequest(BaseModel):
    group_id: str
    amount: float = Field(gt=0)
    group_balance: float = Field(ge=0)
    requester_role: str = "member"
    requester_tenure_days: float = 0
    requester_total_contributed: float = 0
    group_avg_withdrawal: float = 0
    withdrawals_last_7_days: int = 0
    recipient_is_requester: bool = False
    recipient_is_member: bool = True
    hour_of_day: int = Field(default=12, ge=0, le=23)


class ScoreResponse(BaseModel):
    score: float
    level: str
    reasons: list[str]


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}


@app.post("/score", response_model=ScoreResponse)
def score(req: ScoreRequest) -> ScoreResponse:
    result = scorer.score(WithdrawalFeatures(**req.model_dump()))
    return ScoreResponse(score=result.score, level=result.level, reasons=result.reasons)
