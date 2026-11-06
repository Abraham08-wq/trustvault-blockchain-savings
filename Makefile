.PHONY: setup up down logs ps test test-contracts test-backend test-ai deploy-contracts check reset open admin code deploy-testnet prod-up share

setup:            ## one-time setup: checks tools, creates .env, starts stack, deploys contract
	./scripts/setup.sh

up:               ## start everything (redeploys the contract only if it is missing)
	docker compose up -d --build postgres anvil ai
	./scripts/deploy.sh
	docker compose up -d --build api

down:             ## stop everything (data and blockchain state are kept)
	docker compose down

reset:            ## stop and DELETE all ChamaChain data, then run: make setup
	docker compose down -v
	sed -i 's|^CHAIN_CONTRACT_ADDRESS=.*|CHAIN_CONTRACT_ADDRESS=|' .env

logs:             ## follow API logs (Ctrl+C to stop watching)
	docker compose logs -f api

ps:
	docker compose ps

open:             ## open the console in your browser
	xdg-open http://127.0.0.1:47080 >/dev/null 2>&1 || echo "Open http://127.0.0.1:47080 in your browser"

share:            ## free public link so members can use the app on their phones (Ctrl+C to stop)
	./scripts/share.sh

admin:            ## create your personal admin sign-in (PROD=1 on a production server)
	./scripts/create-admin.sh

code:             ## show the latest SMS login codes while SMS is in test mode
	@docker compose logs api 2>/dev/null | grep 'TEST SMS' | tail -5 | sed -E 's/.*"to":"([^"]+)".*code is ([0-9]{6}).*/\1  code \2/' || true

deploy-testnet:   ## anchor to the public Celo Sepolia network (run twice: fund wallet, then deploy)
	./scripts/deploy-testnet.sh

prod-up:          ## start the production stack with HTTPS on a cloud server
	./scripts/prod-up.sh

deploy-contracts: ## deploy ChamaLedger if missing, then restart the API with the address
	./scripts/deploy.sh
	docker compose up -d --force-recreate api

test: test-contracts test-backend test-ai

test-contracts:
	cd contracts && forge test -vv

test-backend:
	cd backend && go test ./...

test-ai:
	docker compose run --rm ai python -m pytest -q

check:            ## run the full system check (proves every protection works)
	./scripts/system-check.sh
