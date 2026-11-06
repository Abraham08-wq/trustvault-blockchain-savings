#!/usr/bin/env bash
# One-time setup for ChamaChain on Linux. Safe to re-run.
set -euo pipefail
cd "$(dirname "$0")/.."

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

info "Checking prerequisites"
command -v git >/dev/null || fail "git is missing: sudo apt install -y git"
command -v curl >/dev/null || fail "curl is missing: sudo apt install -y curl"
command -v jq >/dev/null || fail "jq is missing: sudo apt install -y jq"
command -v docker >/dev/null || fail "Docker is missing: see https://docs.docker.com/engine/install/"
docker compose version >/dev/null 2>&1 || fail "Docker Compose plugin is missing: sudo apt install -y docker-compose-plugin"

# Foundry may already be installed but not yet on PATH in this terminal
export PATH="$HOME/.foundry/bin:$PATH"
if ! command -v forge >/dev/null; then
  info "Installing Foundry (smart contract tools)"
  curl -L https://foundry.paradigm.xyz | bash
  export PATH="$HOME/.foundry/bin:$PATH"
  foundryup
fi

rand_hex() { openssl rand -hex "$1" 2>/dev/null || head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; }

if [[ ! -f .env ]]; then
  info "Creating .env with fresh secrets"
  cp .env.example .env
  sed -i "s|^API_KEY=.*|API_KEY=$(rand_hex 24)|" .env
fi
# Add any settings introduced in newer versions, keeping existing values
while IFS= read -r line; do
  [[ "$line" =~ ^([A-Z_]+)= ]] || continue
  key=${BASH_REMATCH[1]}
  grep -qE "^${key}=" .env || echo "$line" >> .env
done < .env.example
if [[ -z "$(grep -E '^SESSION_SECRET=' .env | cut -d= -f2-)" ]]; then
  sed -i "s|^SESSION_SECRET=.*|SESSION_SECRET=$(rand_hex 32)|" .env
  info "Generated SESSION_SECRET"
fi

info "Installing contract library (forge-std)"
if [[ ! -d contracts/lib/forge-std ]]; then
  git clone --depth 1 https://github.com/foundry-rs/forge-std contracts/lib/forge-std
fi

info "Running smart contract tests"
(cd contracts && forge test)

info "Starting the stack (database, local blockchain, AI service, API)"
docker compose up -d --build postgres anvil ai

./scripts/deploy.sh

# Recreate the API so it picks up the contract address from .env
docker compose up -d --build --force-recreate api
for _ in $(seq 1 60); do
  curl -fsS http://127.0.0.1:47080/health >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS http://127.0.0.1:47080/health >/dev/null || fail "API did not start: run 'docker compose logs api'"

info "Done."
echo
echo "  Console:     http://127.0.0.1:47080"
if [[ "$(docker compose exec -T postgres psql -U chama -d chamachain -tAc 'SELECT COUNT(*) FROM admins' 2>/dev/null)" == "0" ]]; then
  echo "  Next step:   make admin    (create your personal sign-in)"
fi
echo "  System check: make check"
