#!/usr/bin/env bash
# ChamaChain system check: proves every protection works against the running system.
# It creates a clearly labelled check group, attacks it the way a dishonest official would,
# and confirms each attack is stopped. The tampering test restores the record afterwards.
# Usage: make check
set -euo pipefail

cd "$(dirname "$0")/.."
API="${API_URL:-http://127.0.0.1:47080}"
KEY="${API_KEY:-$(grep -E '^API_KEY=' .env | cut -d= -f2-)}"
PSQL="${PSQL:-docker compose exec -T postgres psql -U chama -d chamachain -qtA -v ON_ERROR_STOP=0}"

PASS=0; FAIL=0
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }
ok()   { PASS=$((PASS+1)); printf '  \033[32m✔\033[0m %s\n' "$*"; }
bad()  { FAIL=$((FAIL+1)); printf '  \033[31m✘\033[0m %s\n' "$*"; }
expect() { # description, actual, expected
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (got: $2, expected: $3)"; fi
}
call() { # method path [json]
  local method=$1 path=$2 body=${3:-}
  if [[ -n "$body" ]]; then
    curl -sS -X "$method" "$API$path" -H "X-API-Key: $KEY" -H 'Content-Type: application/json' -d "$body"
  else
    curl -sS -X "$method" "$API$path" -H "X-API-Key: $KEY"
  fi
}

curl -fsS "$API/health" >/dev/null || { echo "ChamaChain API is not running. Start it with: make up"; exit 1; }
SUFFIX=$(date +%H%M%S)

step "Setting up a check group"
GROUP=$(call POST /groups "{\"name\":\"System check $(date '+%Y-%m-%d %H:%M')\",\"currency\":\"KES\",\"approval_threshold\":2}")
GID=$(jq -r .id <<<"$GROUP")
[[ "$GID" != "null" ]] && ok "Group created ($GID)" || { bad "Group creation failed: $GROUP"; exit 1; }
add() { call POST "/groups/$GID/members" "{\"name\":\"$1\",\"phone\":\"$2\",\"role\":\"$3\"}" | jq -r .id; }
# Check-only phone numbers (070 + time + index), unique per run
CHAIR=$(add "Check Chair" "070${SUFFIX}1" chair)
TREASURER=$(add "Check Treasurer" "070${SUFFIX}2" treasurer)
SECRETARY=$(add "Check Secretary" "070${SUFFIX}3" secretary)
MEMBER=$(add "Check Member" "070${SUFFIX}4" member)
TPHONE="25470${SUFFIX}2"; MPHONE="25470${SUFFIX}4"
[[ -n "$CHAIR" && "$CHAIR" != "null" && "$MEMBER" != "null" ]] && ok "Four members added" || bad "Adding members failed"

step "Contributions through M-Pesa"
for M in "$CHAIR" "$TREASURER" "$SECRETARY" "$MEMBER"; do
  call POST "/groups/$GID/contributions/mpesa" "{\"member_id\":\"$M\",\"amount\":5000}" >/dev/null
done
expect "Balance is KES 20,000 after four contributions" "$(call GET "/groups/$GID" | jq -r .balance)" "20000"

step "Attack 1: treasurer tries to send most of the money to herself"
BAD=$(call POST "/groups/$GID/withdrawals" "{\"requested_by\":\"$TREASURER\",\"amount\":18000,\"recipient_phone\":\"$TPHONE\",\"reason\":\"emergency\"}")
BADID=$(jq -r .id <<<"$BAD")
expect "AI engine flags it as high risk" "$(jq -r .risk_level <<<"$BAD")" "high"
expect "One extra approval is required" "$(jq -r .required_approvals <<<"$BAD")" "3"
jq -r '.risk_reasons[]' <<<"$BAD" | sed 's/^/      reason: /'

step "Attack 2: she approves her own request"
R=$(call POST "/withdrawals/$BADID/approve" "{\"member_id\":\"$TREASURER\"}")
expect "Self-approval is refused" "$(jq -r .error <<<"$R")" "forbidden: you cannot approve your own withdrawal"

step "Members stop it"
# 3 approvals are needed from 3 eligible members, so a single rejection makes approval impossible.
R=$(call POST "/withdrawals/$BADID/reject" "{\"member_id\":\"$CHAIR\"}")
expect "The chair's rejection stops the withdrawal" "$(jq -r .status <<<"$R")" "rejected"
R=$(call POST "/withdrawals/$BADID/approve" "{\"member_id\":\"$SECRETARY\"}")
expect "Nobody can approve it afterwards" "$(jq -r .error <<<"$R")" "conflict: withdrawal is already rejected"
expect "Group balance is untouched" "$(call GET "/groups/$GID" | jq -r .available)" "20000"

step "A normal loan goes through"
OK=$(call POST "/groups/$GID/withdrawals" "{\"requested_by\":\"$MEMBER\",\"amount\":3000,\"recipient_phone\":\"$MPHONE\",\"reason\":\"school fees\"}")
OKID=$(jq -r .id <<<"$OK")
expect "AI engine rates it low risk" "$(jq -r .risk_level <<<"$OK")" "low"
call POST "/withdrawals/$OKID/approve" "{\"member_id\":\"$CHAIR\"}" >/dev/null
R=$(call POST "/withdrawals/$OKID/approve" "{\"member_id\":\"$SECRETARY\"}")
expect "Two approvals release the payment" "$(jq -r .status <<<"$R")" "executed"
expect "Balance is now KES 17,000" "$(call GET "/groups/$GID" | jq -r .balance)" "17000"

step "Blockchain anchoring"
V=""
for _ in $(seq 1 10); do
  V=$(call GET "/groups/$GID/ledger/verify")
  [[ "$(jq -r .anchor_matches <<<"$V")" == "true" ]] && break
  sleep 1
done
expect "Ledger is intact" "$(jq -r .valid <<<"$V")" "true"
expect "Ledger matches the on-chain anchor" "$(jq -r .anchor_matches <<<"$V")" "true"
expect "Anchor was read from the blockchain" "$(jq -r .anchor_source <<<"$V")" "blockchain"

step "Basic phone access (USSD)"
U=$(curl -sS -X POST "$API/ussd" -d "sessionId=check&serviceCode=*384*1#&phoneNumber=+$MPHONE&text=1")
[[ "$U" == *"KES 17000"* ]] && ok "USSD balance shows KES 17,000" || bad "USSD balance wrong: $U"

step "Attack 3: a database insider edits an old record"
OUT=$($PSQL -c "UPDATE ledger_entries SET amount=50000 WHERE group_id='$GID' AND seq=1;" 2>&1 || true)
[[ "$OUT" == *"append-only"* ]] && ok "Direct edit is blocked by the database" || bad "Direct edit was not blocked: $OUT"

step "Attack 4: the insider disables the protection and edits anyway"
$PSQL -c "ALTER TABLE ledger_entries DISABLE TRIGGER ledger_no_update; UPDATE ledger_entries SET amount=50000 WHERE group_id='$GID' AND seq=1; ALTER TABLE ledger_entries ENABLE TRIGGER ledger_no_update;" >/dev/null 2>&1 || true
V=$(call GET "/groups/$GID/ledger/verify")
expect "Tampering is detected" "$(jq -r .valid <<<"$V")" "false"
expect "The altered record is pinpointed" "$(jq -r .broken_at_seq <<<"$V")" "1"
$PSQL -c "ALTER TABLE ledger_entries DISABLE TRIGGER ledger_no_update; UPDATE ledger_entries SET amount=5000 WHERE group_id='$GID' AND seq=1; ALTER TABLE ledger_entries ENABLE TRIGGER ledger_no_update;" >/dev/null 2>&1 || true
expect "After restoring the record, the ledger verifies again" "$(call GET "/groups/$GID/ledger/verify" | jq -r .valid)" "true"

echo
if (( FAIL == 0 )); then
  printf '\033[1;32mAll %d checks passed.\033[0m Open the console to see this group: http://127.0.0.1:47080\n' "$PASS"
else
  printf '\033[1;31m%d of %d checks failed.\033[0m See the lines marked ✘ above.\n' "$FAIL" "$((PASS+FAIL))"
  exit 1
fi
