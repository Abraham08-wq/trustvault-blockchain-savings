# ChamaChain

Tamper-proof savings groups for Africa. No single person can move a group's money:
every withdrawal needs approvals from other members, an AI engine flags suspicious requests,
and the ledger is hash-chained and anchored on a blockchain so any edit to history is caught.

## What's inside

| Folder | What it is |
| --- | --- |
| `backend/` | Go API and the **ChamaChain Shield** console: groups, members, hash-chained ledger, approvals, M-Pesa, SMS login, USSD, blockchain anchoring, admin accounts and audit log |
| `mobile/` | **Member app** for Android (Flutter): phone login, balances, contribute, request money, vote |
| `ai-service/` | Python fraud scoring: explainable rules plus an IsolationForest anomaly model |
| `contracts/` | Solidity contracts (Foundry): `ChamaLedger` anchors ledger hashes; `ChamaVault` for member-signed on-chain votes |
| `scripts/` | Setup, admin accounts, system check, public blockchain deployment, production start |
| `deploy/` | HTTPS reverse proxy configuration for going online |
| `.github/workflows/` | Builds and tests the Android app in the cloud |

## Install (Ubuntu or Debian)

```bash
sudo apt update && sudo apt install -y git curl jq make openssl unzip qrencode
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER && newgrp docker

unzip chamachain.zip && cd chamachain
chmod +x scripts/*.sh
make setup
make admin
```

`make admin` creates your personal sign-in: a username, a password (stored as an Argon2id hash) and a QR code for an authenticator app such as Google Authenticator. Then open **http://127.0.0.1:47080** and sign in.

## Members on their phones (free)

The member app is a web app that installs to the home screen on Android and iPhone. Nothing to download from an app store.

```bash
make share
```

This prints a public link like `https://some-words.trycloudflare.com/app/` and a QR code. Send it to members by WhatsApp. They open it, sign in with their phone number and an SMS code, then tap **Install app** (Android) or **Share > Add to Home Screen** (iPhone). While SMS is in test mode, get login codes with `make code` in a second terminal.

The link works while `make share` runs and your laptop is on; each run gives a new link. It is free and needs no account. For a permanent address, see "Going online".

## Who uses what

| Person | Uses | Signs in with |
| --- | --- | --- |
| You and group officials | ChamaChain Shield console in a browser | Username, password and authenticator code |
| Members with smartphones | ChamaChain member app (web, `/app/`), or the Android app in `mobile/` | Phone number and SMS code |
| Members with basic phones | USSD menu | Their registered phone number |
| Scripts and integrations | The API | Service access key (`API_KEY` in `.env`) |

## Everyday commands

```bash
make up        # start everything
make down      # stop (data and blockchain state are kept)
make share     # free public link for the member app (Ctrl+C to stop)
make admin     # create another admin sign-in
make check     # system check: attacks a labelled check group and confirms each attack is stopped
make code      # show recent SMS login codes while SMS is in test mode
make logs      # follow API logs (Ctrl+C to stop watching)
make test      # contract, backend and AI tests
make reset     # DELETE all ChamaChain data, then run: make setup
```

ChamaChain uses only these ports on your computer, all bound to 127.0.0.1: **47080** (console and API), **47432** (database), **47545** (local blockchain), **47800** (AI engine). It manages only its own containers, network and volumes.

## Security built in

- **Admins:** Argon2id passwords, authenticator codes that work once, 5 failed attempts lock the account for 15 minutes, sign-in rate limits, HttpOnly SameSite=Strict session cookies, anti-forgery header on every change.
- **Members:** 6-digit SMS codes stored only as keyed hashes, valid 5 minutes, 5 attempts, 3 codes per 15 minutes; the API never reveals whether a number is registered; session tokens stored only as hashes.
- **Every change** is written to an append-only audit log (who, what, when, from which IP, and whether a vote came from the app, USSD or console).
- **Money:** requesters can never approve their own withdrawal; high-risk requests need one extra approval; pending requests reserve funds; members only see and act on their own groups.
- **Ledger:** the database blocks edits, each record's hash covers the one before it, and the head is anchored to a smart contract that verification reads directly.
- **Console:** strict Content Security Policy with a per-request nonce.

## Android app (for the Play Store later)

The web member app covers phones today. `mobile/` holds a native Android version for when you publish on Google Play; see [mobile/README.md](mobile/README.md).

## Public blockchain

```bash
make deploy-testnet
```

The first run creates a wallet and shows its address. Get free test CELO for it from https://faucet.celo.org/celo-sepolia, then run `make deploy-testnet` again. ChamaChain then anchors every group's ledger on Celo Sepolia, where anyone can check it at https://celo-sepolia.blockscout.com. The wallet key is saved in `.testnet-wallet`; back it up and never share it.

## Going online

You need a cloud server (Ubuntu, 2 GB RAM or more, for example from DigitalOcean, Hetzner, AWS Lightsail or a Kenyan host) and a domain name.

1. At your domain provider, create an **A record** (for example `app.yourdomain.com`) pointing to the server's IP address.
2. On the server, install Docker (the same commands as Install), copy this folder there, and copy your `.env` from your laptop.
3. In `.env` on the server set `DOMAIN=app.yourdomain.com` and a strong `POSTGRES_PASSWORD` (`openssl rand -hex 24`). Make sure the `CHAIN_` values point to the public blockchain (from `make deploy-testnet`).
4. Run `make prod-up`. HTTPS certificates are issued automatically.
5. Run `make admin PROD=1` to create your admin sign-in on the server.
6. Members enter `https://app.yourdomain.com` in the member app.

Only ports 80 and 443 are open in production; the database and AI engine are not reachable from the internet.

## Live payments and SMS

| Service | Where | Put in `.env` |
| --- | --- | --- |
| M-Pesa (Safaricom Daraja) | https://developer.safaricom.co.ke: create an app, use the sandbox keys first | `MPESA_CONSUMER_KEY`, `MPESA_CONSUMER_SECRET`, `MPESA_PASSKEY`, `MPESA_SHORTCODE`, `MPESA_CALLBACK_URL=https://your-domain/mpesa/callback` |
| SMS login codes (Africa's Talking) | https://account.africastalking.com: sandbox is free; live SMS needs a funded account | `AT_USERNAME`, `AT_API_KEY`, `AT_ENV=production` when live |
| USSD menu (Africa's Talking) | Request a USSD service code and set its callback to `https://your-domain/ussd` | nothing |

Restart with `make up` (or `make prod-up` on the server) after changing `.env`. Until keys are added, payments and SMS run in test mode and the console says so.

## Before handling real money

- Independent smart contract and application security audit.
- A legal opinion under Kenya's Virtual Asset Service Providers Act, and registration with the Office of the Data Protection Commissioner.
- Safaricom approval for B2C payouts (until then payout references are marked `TEST-PAYOUT`).
- Move from Celo Sepolia to Celo mainnet (chain 42220) with a funded wallet.

## API

Admin endpoints accept a signed-in admin session or the `X-API-Key` header. Member endpoints need `Authorization: Bearer <token>` from the SMS login.

| Method | Path | Notes |
| --- | --- | --- |
| POST | `/auth/otp/request` | `{"phone"}` sends an SMS code |
| POST | `/auth/otp/verify` | `{"phone","code","device"}` returns a member token |
| GET | `/me` | the member's groups and pending votes |
| GET | `/me/groups/{id}`, `/activity`, `/verify`, `/withdrawals` | one group, as the member sees it |
| POST | `/me/groups/{id}/contributions` | `{"amount"}` M-Pesa prompt to the member's phone |
| POST | `/me/groups/{id}/withdrawals` | `{"amount","recipient_phone","reason"}` |
| GET | `/me/approvals` | requests waiting for this member's vote |
| POST | `/me/withdrawals/{id}/approve`, `/reject`, `/cancel` | the member votes or cancels as themselves |
| POST | `/admin/login`, `/admin/logout` | console sign-in |
| GET | `/system`, `/stats`, `/audit` | live component status, totals, audit log |
| GET, POST | `/groups`, `/groups/{id}`, `/groups/{id}/members`, `/groups/{id}/contributions`, `/groups/{id}/withdrawals`, `/groups/{id}/ledger`, `/groups/{id}/ledger/verify`, `/groups/{id}/anchors` | administration |
| POST | `/withdrawals/{id}/approve`, `/reject`, `/cancel` | `{"member_id"}` votes recorded by an official |
| POST | `/ussd`, `/mpesa/callback` | Africa's Talking USSD and Safaricom callbacks |

## Back up to GitHub

```bash
cd chamachain
git init && git add . && git commit -m "ChamaChain"
# Create an empty PRIVATE repository on github.com, then:
git remote add origin https://github.com/<your-username>/chamachain.git
git branch -M main && git push -u origin main
```

`.env` and `.testnet-wallet` are excluded automatically, so your secrets are never uploaded.
