// Package core holds ChamaChain's business rules: groups, members, the hash-chained ledger,
// and multi-approval withdrawals.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chamachain/backend/internal/chain"
	"github.com/chamachain/backend/internal/ledger"
	"github.com/chamachain/backend/internal/mpesa"
	"github.com/chamachain/backend/internal/risk"
	"github.com/chamachain/backend/internal/sms"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrValidation = errors.New("validation failed")
	ErrConflict   = errors.New("conflict")
	ErrForbidden  = errors.New("forbidden")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, a...))
}

type Service struct {
	db       *pgxpool.Pool
	risk     *risk.Client
	mpesa    *mpesa.Client
	anchorer *chain.Anchorer // nil when anchoring is disabled
	sms      *sms.Client
	secret   []byte // keys one-time code hashes
	log      *slog.Logger
	anchorMu sync.Mutex
	wg       sync.WaitGroup
}

func NewService(db *pgxpool.Pool, r *risk.Client, m *mpesa.Client, a *chain.Anchorer, s *sms.Client, secret []byte, log *slog.Logger) *Service {
	return &Service{db: db, risk: r, mpesa: m, anchorer: a, sms: s, secret: secret, log: log}
}

// SMSTestMode reports whether text messages are only written to the log.
func (s *Service) SMSTestMode() bool { return s.sms.TestMode() }

func validChannel(c string) string {
	switch c {
	case "console", "app", "ussd", "api":
		return c
	}
	return "api"
}

// Wait blocks until background anchoring jobs finish (used on shutdown and in tests).
func (s *Service) Wait() { s.wg.Wait() }

// ---------- types ----------

type Group struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Currency          string    `json:"currency"`
	ApprovalThreshold int       `json:"approval_threshold"`
	CreatedAt         time.Time `json:"created_at"`
}

type Member struct {
	ID       string    `json:"id"`
	GroupID  string    `json:"group_id"`
	Name     string    `json:"name"`
	Phone    string    `json:"phone"`
	Role     string    `json:"role"`
	Active   bool      `json:"active"`
	JoinedAt time.Time `json:"joined_at"`
}

type GroupDetail struct {
	Group
	Balance          int64    `json:"balance"`
	PendingOutflows  int64    `json:"pending_withdrawals"`
	Available        int64    `json:"available"`
	Members          []Member `json:"members"`
	LedgerEntries    int64    `json:"ledger_entries"`
	LastAnchoredSeq  int64    `json:"last_anchored_entries"`
	LastAnchorTxHash string   `json:"last_anchor_tx,omitempty"`
}

type LedgerEntry struct {
	Seq       int64     `json:"seq"`
	Type      string    `json:"type"`
	MemberID  *string   `json:"member_id,omitempty"`
	Amount    int64     `json:"amount"`
	Reference string    `json:"reference"`
	PrevHash  string    `json:"prev_hash"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
}

type Withdrawal struct {
	ID                string     `json:"id"`
	GroupID           string     `json:"group_id"`
	RequestedBy       string     `json:"requested_by"`
	Amount            int64      `json:"amount"`
	RecipientPhone    string     `json:"recipient_phone"`
	Reason            string     `json:"reason"`
	Status            string     `json:"status"`
	RequiredApprovals int        `json:"required_approvals"`
	Approvals         []string   `json:"approvals"`
	Rejections        []string   `json:"rejections"`
	RiskScore         float64    `json:"risk_score"`
	RiskLevel         string     `json:"risk_level"`
	RiskReasons       []string   `json:"risk_reasons"`
	PayoutReference   string     `json:"payout_reference,omitempty"`
	LedgerSeq         *int64     `json:"ledger_seq,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	ExecutedAt        *time.Time `json:"executed_at,omitempty"`
}

// ---------- groups & members ----------

func (s *Service) CreateGroup(ctx context.Context, name, currency string, threshold int) (Group, error) {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 120 {
		return Group{}, invalid("name must be 2-120 characters")
	}
	if threshold < 1 || threshold > 20 {
		return Group{}, invalid("approval_threshold must be between 1 and 20")
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "KES"
	}
	if len(currency) != 3 {
		return Group{}, invalid("currency must be a 3-letter code like KES, UGX, NGN")
	}
	var g Group
	err := s.db.QueryRow(ctx, `INSERT INTO groups(name, currency, approval_threshold) VALUES ($1,$2,$3)
		RETURNING id, name, currency, approval_threshold, created_at`, name, currency, threshold).
		Scan(&g.ID, &g.Name, &g.Currency, &g.ApprovalThreshold, &g.CreatedAt)
	return g, err
}

func (s *Service) AddMember(ctx context.Context, groupID, name, phone, role string) (Member, error) {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 120 {
		return Member{}, invalid("name must be 2-120 characters")
	}
	p, err := mpesa.NormalizePhone(phone)
	if err != nil {
		return Member{}, invalid("%v", err)
	}
	if role == "" {
		role = "member"
	}
	switch role {
	case "chair", "treasurer", "secretary", "member":
	default:
		return Member{}, invalid("role must be chair, treasurer, secretary or member")
	}
	if _, err := s.getGroup(ctx, s.db, groupID); err != nil {
		return Member{}, err
	}
	var m Member
	err = s.db.QueryRow(ctx, `INSERT INTO members(group_id, name, phone, role) VALUES ($1,$2,$3,$4)
		RETURNING id, group_id, name, phone, role, active, joined_at`, groupID, name, p, role).
		Scan(&m.ID, &m.GroupID, &m.Name, &m.Phone, &m.Role, &m.Active, &m.JoinedAt)
	if isUniqueViolation(err) {
		return Member{}, fmt.Errorf("%w: this phone number is already a member of the group", ErrConflict)
	}
	return m, err
}

func (s *Service) GetGroup(ctx context.Context, groupID string) (GroupDetail, error) {
	g, err := s.getGroup(ctx, s.db, groupID)
	if err != nil {
		return GroupDetail{}, err
	}
	d := GroupDetail{Group: g, Members: []Member{}}
	rows, err := s.db.Query(ctx, `SELECT id, group_id, name, phone, role, active, joined_at
		FROM members WHERE group_id=$1 ORDER BY joined_at`, groupID)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.GroupID, &m.Name, &m.Phone, &m.Role, &m.Active, &m.JoinedAt); err != nil {
			rows.Close()
			return d, err
		}
		d.Members = append(d.Members, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return d, err
	}
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0), COUNT(*) FROM ledger_entries WHERE group_id=$1`, groupID).
		Scan(&d.Balance, &d.LedgerEntries); err != nil {
		return d, err
	}
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM withdrawals WHERE group_id=$1 AND status='pending'`, groupID).
		Scan(&d.PendingOutflows); err != nil {
		return d, err
	}
	d.Available = d.Balance - d.PendingOutflows
	_ = s.db.QueryRow(ctx, `SELECT entries, tx_hash FROM anchors WHERE group_id=$1 ORDER BY entries DESC LIMIT 1`, groupID).
		Scan(&d.LastAnchoredSeq, &d.LastAnchorTxHash)
	return d, nil
}

// ---------- system status, network stats, anchors ----------

type ComponentStatus struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Ms     int64  `json:"latency_ms"`
}

type SystemStatus struct {
	Database     ComponentStatus `json:"database"`
	Blockchain   ComponentStatus `json:"blockchain"`
	AI           ComponentStatus `json:"ai"`
	Payments     ComponentStatus `json:"payments"`
	Block        uint64          `json:"block_number"`
	ChainID      int64           `json:"chain_id"`
	Contract     string          `json:"contract,omitempty"`
	Relayer      string          `json:"relayer,omitempty"`
	PaymentsMode string          `json:"payments_mode"` // live | test
	CheckedAt    time.Time       `json:"checked_at"`
}

// SystemStatus runs real checks against every component.
func (s *Service) SystemStatus(ctx context.Context) SystemStatus {
	st := SystemStatus{CheckedAt: time.Now().UTC()}

	t := time.Now()
	if err := s.db.Ping(ctx); err != nil {
		st.Database = ComponentStatus{OK: false, Detail: "database unreachable"}
	} else {
		var groups int64
		_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM groups`).Scan(&groups)
		st.Database = ComponentStatus{OK: true, Detail: fmt.Sprintf("ledger store online, %d groups", groups)}
	}
	st.Database.Ms = time.Since(t).Milliseconds()

	t = time.Now()
	if s.anchorer == nil {
		st.Blockchain = ComponentStatus{OK: false, Detail: "anchoring not configured"}
	} else {
		st.ChainID, st.Contract, st.Relayer = s.anchorer.ChainID(), s.anchorer.Contract(), s.anchorer.From()
		n, err := s.anchorer.BlockNumber(ctx)
		switch {
		case err != nil:
			st.Blockchain = ComponentStatus{OK: false, Detail: "chain node unreachable"}
		case s.anchorer.CheckContract(ctx) != nil:
			st.Block = n
			st.Blockchain = ComponentStatus{OK: false, Detail: "ledger contract missing on chain"}
		default:
			st.Block = n
			st.Blockchain = ComponentStatus{OK: true, Detail: fmt.Sprintf("chain %d, block %d", st.ChainID, n)}
		}
	}
	st.Blockchain.Ms = time.Since(t).Milliseconds()

	t = time.Now()
	if s.risk.Healthy(ctx) {
		st.AI = ComponentStatus{OK: true, Detail: "fraud model loaded"}
	} else {
		st.AI = ComponentStatus{OK: false, Detail: "AI engine offline, rules-only protection active"}
	}
	st.AI.Ms = time.Since(t).Milliseconds()

	if s.mpesa.TestMode() {
		st.PaymentsMode = "test"
		st.Payments = ComponentStatus{OK: true, Detail: "test payment mode, add Safaricom keys for live M-Pesa"}
	} else {
		st.PaymentsMode = "live"
		st.Payments = ComponentStatus{OK: true, Detail: "M-Pesa Daraja connected"}
	}
	return st
}

type NetworkStats struct {
	Groups             int64 `json:"groups"`
	Members            int64 `json:"members"`
	Protected          int64 `json:"protected_balance"`
	LedgerEntries      int64 `json:"ledger_entries"`
	PendingWithdrawals int64 `json:"pending_withdrawals"`
	FlaggedHighRisk    int64 `json:"flagged_high_risk"`
	BlockedWithdrawals int64 `json:"blocked_withdrawals"`
	Anchors            int64 `json:"anchors"`
}

func (s *Service) Stats(ctx context.Context) (NetworkStats, error) {
	var n NetworkStats
	err := s.db.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM groups),
		(SELECT COUNT(*) FROM members WHERE active),
		(SELECT COALESCE(SUM(amount),0) FROM ledger_entries),
		(SELECT COUNT(*) FROM ledger_entries),
		(SELECT COUNT(*) FROM withdrawals WHERE status='pending'),
		(SELECT COUNT(*) FROM withdrawals WHERE risk_level='high'),
		(SELECT COUNT(*) FROM withdrawals WHERE status='rejected'),
		(SELECT COUNT(*) FROM anchors)`).
		Scan(&n.Groups, &n.Members, &n.Protected, &n.LedgerEntries, &n.PendingWithdrawals, &n.FlaggedHighRisk, &n.BlockedWithdrawals, &n.Anchors)
	return n, err
}

type Anchor struct {
	Entries   int64     `json:"entries"`
	Head      string    `json:"head"`
	TxHash    string    `json:"tx_hash"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) Anchors(ctx context.Context, groupID string) ([]Anchor, error) {
	if _, err := s.getGroup(ctx, s.db, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT entries, head, tx_hash, created_at FROM anchors WHERE group_id=$1 ORDER BY entries DESC LIMIT 50`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Anchor{}
	for rows.Next() {
		var a Anchor
		if err := rows.Scan(&a.Entries, &a.Head, &a.TxHash, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type GroupSummary struct {
	Group
	Balance     int64 `json:"balance"`
	Members     int   `json:"members"`
	PendingWith int   `json:"pending_withdrawals"`
}

// ListGroups returns every group with its balance, newest first.
func (s *Service) ListGroups(ctx context.Context) ([]GroupSummary, error) {
	rows, err := s.db.Query(ctx, `SELECT g.id, g.name, g.currency, g.approval_threshold, g.created_at,
		COALESCE((SELECT SUM(amount) FROM ledger_entries l WHERE l.group_id=g.id),0),
		(SELECT COUNT(*) FROM members m WHERE m.group_id=g.id AND m.active),
		(SELECT COUNT(*) FROM withdrawals w WHERE w.group_id=g.id AND w.status='pending')
		FROM groups g ORDER BY g.created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GroupSummary{}
	for rows.Next() {
		var g GroupSummary
		if err := rows.Scan(&g.ID, &g.Name, &g.Currency, &g.ApprovalThreshold, &g.CreatedAt, &g.Balance, &g.Members, &g.PendingWith); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// MembersByPhone finds a person's memberships across groups (used by USSD).
func (s *Service) MembersByPhone(ctx context.Context, phone string) ([]Member, error) {
	p, err := mpesa.NormalizePhone(phone)
	if err != nil {
		return nil, invalid("%v", err)
	}
	rows, err := s.db.Query(ctx, `SELECT id, group_id, name, phone, role, active, joined_at
		FROM members WHERE phone=$1 AND active ORDER BY joined_at`, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.GroupID, &m.Name, &m.Phone, &m.Role, &m.Active, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) MemberContributed(ctx context.Context, memberID string) (int64, error) {
	var total int64
	err := s.db.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM ledger_entries
		WHERE member_id=$1 AND entry_type='contribution'`, memberID).Scan(&total)
	return total, err
}

// ---------- ledger ----------

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Service) getGroup(ctx context.Context, q querier, id string) (Group, error) {
	var g Group
	err := q.QueryRow(ctx, `SELECT id, name, currency, approval_threshold, created_at FROM groups WHERE id=$1`, id).
		Scan(&g.ID, &g.Name, &g.Currency, &g.ApprovalThreshold, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return g, fmt.Errorf("%w: group", ErrNotFound)
	}
	return g, err
}

func (s *Service) getMember(ctx context.Context, q querier, id string) (Member, error) {
	var m Member
	err := q.QueryRow(ctx, `SELECT id, group_id, name, phone, role, active, joined_at FROM members WHERE id=$1`, id).
		Scan(&m.ID, &m.GroupID, &m.Name, &m.Phone, &m.Role, &m.Active, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return m, fmt.Errorf("%w: member", ErrNotFound)
	}
	return m, err
}

// lockGroup serialises all money movements of one group.
func lockGroup(ctx context.Context, tx pgx.Tx, groupID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM groups WHERE id=$1 FOR UPDATE`, groupID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return fmt.Errorf("%w: group", ErrNotFound)
	}
	return err
}

// appendEntry adds a hash-chained entry. The caller must hold lockGroup in the same transaction.
func appendEntry(ctx context.Context, tx pgx.Tx, groupID, entryType string, memberID *string, amount int64, ref string) (LedgerEntry, error) {
	var lastSeq int64
	var lastHash string
	err := tx.QueryRow(ctx, `SELECT seq, hash FROM ledger_entries WHERE group_id=$1 ORDER BY seq DESC LIMIT 1`, groupID).
		Scan(&lastSeq, &lastHash)
	if errors.Is(err, pgx.ErrNoRows) {
		lastSeq, lastHash = 0, ledger.GenesisHash
	} else if err != nil {
		return LedgerEntry{}, err
	}
	e := LedgerEntry{
		Seq: lastSeq + 1, Type: entryType, MemberID: memberID, Amount: amount, Reference: ref,
		PrevHash: lastHash, CreatedAt: ledger.Normalize(time.Now()),
	}
	e.Hash = ledger.Hash(toLedgerEntry(groupID, e))
	_, err = tx.Exec(ctx, `INSERT INTO ledger_entries(group_id, seq, entry_type, member_id, amount, reference, prev_hash, hash, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, groupID, e.Seq, e.Type, e.MemberID, e.Amount, e.Reference, e.PrevHash, e.Hash, e.CreatedAt)
	return e, err
}

func toLedgerEntry(groupID string, e LedgerEntry) ledger.Entry {
	mid := ""
	if e.MemberID != nil {
		mid = *e.MemberID
	}
	return ledger.Entry{GroupID: groupID, Seq: e.Seq, Type: e.Type, MemberID: mid, Amount: e.Amount,
		Reference: e.Reference, PrevHash: e.PrevHash, CreatedAt: e.CreatedAt}
}

func (s *Service) Ledger(ctx context.Context, groupID string) ([]LedgerEntry, error) {
	if _, err := s.getGroup(ctx, s.db, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT seq, entry_type, member_id, amount, reference, prev_hash, hash, created_at
		FROM ledger_entries WHERE group_id=$1 ORDER BY seq`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.Seq, &e.Type, &e.MemberID, &e.Amount, &e.Reference, &e.PrevHash, &e.Hash, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.CreatedAt = ledger.Normalize(e.CreatedAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

type VerifyReport struct {
	ledger.VerifyResult
	AnchoredEntries int64  `json:"anchored_entries"`
	AnchoredHead    string `json:"anchored_head,omitempty"`
	AnchorTx        string `json:"anchor_tx,omitempty"`
	AnchorMatches   *bool  `json:"anchor_matches,omitempty"`
	Source          string `json:"anchor_source"` // blockchain | database
	Warning         string `json:"warning,omitempty"`
}

// VerifyLedger recomputes every hash and compares the chain with the last on-chain anchor.
func (s *Service) VerifyLedger(ctx context.Context, groupID string) (VerifyReport, error) {
	entries, err := s.Ledger(ctx, groupID)
	if err != nil {
		return VerifyReport{}, err
	}
	le := make([]ledger.Entry, len(entries))
	hashes := make([]string, len(entries))
	for i, e := range entries {
		le[i] = toLedgerEntry(groupID, e)
		hashes[i] = e.Hash
	}
	rep := VerifyReport{VerifyResult: ledger.Verify(le, hashes), Source: "database"}
	_ = s.db.QueryRow(ctx, `SELECT entries, head, tx_hash FROM anchors WHERE group_id=$1 ORDER BY entries DESC LIMIT 1`, groupID).
		Scan(&rep.AnchoredEntries, &rep.AnchoredHead, &rep.AnchorTx)

	// Prefer the smart contract itself as the source of truth for the anchor.
	if s.anchorer == nil {
		rep.Warning = "blockchain anchoring is turned off; only the hash chain was checked"
	} else {
		head, n, err := s.anchorer.Latest(ctx, groupID)
		switch {
		case err != nil:
			s.log.Warn("could not read on-chain anchor", "err", err)
			rep.Warning = "could not read the blockchain: " + err.Error()
		case n > 0:
			rep.AnchoredEntries, rep.AnchoredHead, rep.Source = int64(n), head, "blockchain"
		case len(entries) > 0:
			rep.Warning = "the blockchain has no record for this group yet"
		}
	}
	if rep.AnchoredEntries > 0 {
		match := rep.AnchoredEntries <= int64(len(entries)) && entries[rep.AnchoredEntries-1].Hash == rep.AnchoredHead
		rep.AnchorMatches = &match
		if !match {
			rep.Valid = false
			rep.BrokenWhy = "ledger no longer matches the blockchain anchor: records were altered or deleted"
		}
	}
	return rep, nil
}

// ---------- contributions ----------

func (s *Service) RecordContribution(ctx context.Context, groupID, memberID string, amount int64, reference string) (LedgerEntry, error) {
	if amount <= 0 {
		return LedgerEntry{}, invalid("amount must be greater than zero")
	}
	reference = strings.TrimSpace(reference)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return LedgerEntry{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := lockGroup(ctx, tx, groupID); err != nil {
		return LedgerEntry{}, err
	}
	m, err := s.getMember(ctx, tx, memberID)
	if err != nil {
		return LedgerEntry{}, err
	}
	if m.GroupID != groupID || !m.Active {
		return LedgerEntry{}, invalid("member is not an active member of this group")
	}
	if reference != "" {
		var dup bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ledger_entries WHERE group_id=$1 AND reference=$2)`, groupID, reference).Scan(&dup); err != nil {
			return LedgerEntry{}, err
		}
		if dup {
			return LedgerEntry{}, fmt.Errorf("%w: reference %s was already recorded", ErrConflict, reference)
		}
	}
	e, err := appendEntry(ctx, tx, groupID, "contribution", &memberID, amount, reference)
	if err != nil {
		return LedgerEntry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LedgerEntry{}, err
	}
	s.anchorAsync(groupID)
	return e, nil
}

// StartMpesaContribution sends an STK push to the member's phone. In test payment mode the payment
// is confirmed immediately, so the full flow runs without Safaricom credentials.
func (s *Service) StartMpesaContribution(ctx context.Context, groupID, memberID string, amount int64) (string, bool, error) {
	if amount <= 0 {
		return "", false, invalid("amount must be greater than zero")
	}
	g, err := s.getGroup(ctx, s.db, groupID)
	if err != nil {
		return "", false, err
	}
	m, err := s.getMember(ctx, s.db, memberID)
	if err != nil {
		return "", false, err
	}
	if m.GroupID != groupID || !m.Active {
		return "", false, invalid("member is not an active member of this group")
	}
	checkoutID, err := s.mpesa.STKPush(ctx, m.Phone, amount, "CHAMA", "Chama contribution")
	if err != nil {
		return "", false, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO stk_requests(checkout_request_id, group_id, member_id, amount) VALUES ($1,$2,$3,$4)`,
		checkoutID, g.ID, m.ID, amount); err != nil {
		return "", false, err
	}
	if s.mpesa.TestMode() {
		err = s.HandleMpesaCallback(ctx, mpesa.CallbackResult{
			CheckoutRequestID: checkoutID, Success: true, ResultDesc: "test payment", ReceiptNumber: checkoutID, Amount: amount,
		})
		return checkoutID, true, err
	}
	return checkoutID, false, nil
}

// HandleMpesaCallback is idempotent: repeated callbacks for the same request are ignored.
func (s *Service) HandleMpesaCallback(ctx context.Context, cb mpesa.CallbackResult) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var groupID, memberID, status string
	var amount int64
	err = tx.QueryRow(ctx, `SELECT group_id, member_id, amount, status FROM stk_requests WHERE checkout_request_id=$1 FOR UPDATE`,
		cb.CheckoutRequestID).Scan(&groupID, &memberID, &amount, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: unknown checkout request", ErrNotFound)
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return nil
	}
	if !cb.Success {
		_, err = tx.Exec(ctx, `UPDATE stk_requests SET status='failed', result_desc=$2 WHERE checkout_request_id=$1`, cb.CheckoutRequestID, cb.ResultDesc)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if cb.Amount > 0 && cb.Amount != amount {
		s.log.Warn("mpesa amount differs from request; recording amount actually paid", "requested", amount, "paid", cb.Amount)
		amount = cb.Amount
	}
	if err := lockGroup(ctx, tx, groupID); err != nil {
		return err
	}
	ref := cb.ReceiptNumber
	if ref == "" {
		ref = cb.CheckoutRequestID
	}
	if _, err := appendEntry(ctx, tx, groupID, "contribution", &memberID, amount, ref); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE stk_requests SET status='completed', result_desc=$2 WHERE checkout_request_id=$1`,
		cb.CheckoutRequestID, cb.ResultDesc); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.anchorAsync(groupID)
	return nil
}

// ---------- withdrawals ----------

func (s *Service) RequestWithdrawal(ctx context.Context, groupID, requesterID string, amount int64, recipientPhone, reason string) (Withdrawal, error) {
	if amount <= 0 {
		return Withdrawal{}, invalid("amount must be greater than zero")
	}
	g, err := s.getGroup(ctx, s.db, groupID)
	if err != nil {
		return Withdrawal{}, err
	}
	requester, err := s.getMember(ctx, s.db, requesterID)
	if err != nil {
		return Withdrawal{}, err
	}
	if requester.GroupID != groupID || !requester.Active {
		return Withdrawal{}, invalid("requester is not an active member of this group")
	}
	recipient, err := mpesa.NormalizePhone(recipientPhone)
	if err != nil {
		return Withdrawal{}, invalid("%v", err)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 300 {
		return Withdrawal{}, invalid("reason must be at most 300 characters")
	}

	detail, err := s.GetGroup(ctx, groupID)
	if err != nil {
		return Withdrawal{}, err
	}
	if amount > detail.Available {
		return Withdrawal{}, invalid("insufficient available balance (%d %s available)", detail.Available, g.Currency)
	}
	activeOthers := 0
	recipientIsMember := false
	for _, m := range detail.Members {
		if m.Active && m.ID != requesterID {
			activeOthers++
		}
		if m.Active && m.Phone == recipient {
			recipientIsMember = true
		}
	}

	feat, err := s.riskFeatures(ctx, groupID, requester, amount, detail.Balance)
	if err != nil {
		return Withdrawal{}, err
	}
	feat.RecipientIsRequester = recipient == requester.Phone
	feat.RecipientIsMember = recipientIsMember
	score := s.risk.Score(ctx, feat)

	required := g.ApprovalThreshold
	if score.Level == "high" {
		required++ // risky requests need one extra approver
		score.Reasons = append(score.Reasons, "high risk: one extra approval required")
	}
	if required > activeOthers {
		return Withdrawal{}, invalid("this withdrawal needs %d approvals but the group has only %d other active members", required, activeOthers)
	}
	reasonsJSON, _ := json.Marshal(score.Reasons)

	var w Withdrawal
	err = s.db.QueryRow(ctx, `INSERT INTO withdrawals(group_id, requested_by, amount, recipient_phone, reason, required_approvals, risk_score, risk_level, risk_reasons)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		groupID, requesterID, amount, recipient, reason, required, score.Score, score.Level, reasonsJSON).Scan(&w.ID)
	if err != nil {
		return Withdrawal{}, err
	}
	return s.GetWithdrawal(ctx, w.ID)
}

func (s *Service) riskFeatures(ctx context.Context, groupID string, m Member, amount, balance int64) (risk.Request, error) {
	r := risk.Request{
		GroupID: groupID, Amount: amount, GroupBalance: balance, RequesterRole: m.Role,
		RequesterTenureDays: time.Since(m.JoinedAt).Hours() / 24,
		HourOfDay:           time.Now().In(time.FixedZone("EAT", 3*3600)).Hour(),
	}
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM ledger_entries WHERE member_id=$1 AND entry_type='contribution'`, m.ID).
		Scan(&r.RequesterContributed); err != nil {
		return r, err
	}
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(AVG(-amount),0) FROM ledger_entries WHERE group_id=$1 AND entry_type='withdrawal'`, groupID).
		Scan(&r.AvgWithdrawal); err != nil {
		return r, err
	}
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM withdrawals WHERE group_id=$1 AND created_at > now() - interval '7 days'`, groupID).
		Scan(&r.WithdrawalsLast7Days); err != nil {
		return r, err
	}
	return r, nil
}

// ApproveWithdrawal records one approval. When enough approvals exist, the withdrawal is
// written to the ledger and paid out.
func (s *Service) ApproveWithdrawal(ctx context.Context, withdrawalID, memberID, channel string) (Withdrawal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Withdrawal{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var groupID, requestedBy, status, phone string
	var amount int64
	var required int
	err = tx.QueryRow(ctx, `SELECT group_id, requested_by, status, amount, required_approvals, recipient_phone
		FROM withdrawals WHERE id=$1`, withdrawalID).Scan(&groupID, &requestedBy, &status, &amount, &required, &phone)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return Withdrawal{}, fmt.Errorf("%w: withdrawal", ErrNotFound)
	}
	if err != nil {
		return Withdrawal{}, err
	}
	// Lock the group first (same order as every other money movement) to avoid deadlocks.
	if err := lockGroup(ctx, tx, groupID); err != nil {
		return Withdrawal{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT status FROM withdrawals WHERE id=$1 FOR UPDATE`, withdrawalID).Scan(&status); err != nil {
		return Withdrawal{}, err
	}
	if status != "pending" {
		return Withdrawal{}, fmt.Errorf("%w: withdrawal is already %s", ErrConflict, status)
	}
	approver, err := s.getMember(ctx, tx, memberID)
	if err != nil {
		return Withdrawal{}, err
	}
	if approver.GroupID != groupID || !approver.Active {
		return Withdrawal{}, fmt.Errorf("%w: approver is not an active member of this group", ErrForbidden)
	}
	if memberID == requestedBy {
		return Withdrawal{}, fmt.Errorf("%w: you cannot approve your own withdrawal", ErrForbidden)
	}
	_, err = tx.Exec(ctx, `INSERT INTO withdrawal_approvals(withdrawal_id, member_id, decision, channel) VALUES ($1,$2,'approve',$3)`, withdrawalID, memberID, validChannel(channel))
	if isUniqueViolation(err) {
		return Withdrawal{}, fmt.Errorf("%w: you already voted on this withdrawal", ErrConflict)
	}
	if err != nil {
		return Withdrawal{}, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM withdrawal_approvals WHERE withdrawal_id=$1 AND decision='approve'`, withdrawalID).Scan(&count); err != nil {
		return Withdrawal{}, err
	}

	executed := false
	if count >= required {
		var balance int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM ledger_entries WHERE group_id=$1`, groupID).Scan(&balance); err != nil {
			return Withdrawal{}, err
		}
		if amount > balance {
			return Withdrawal{}, invalid("group balance is now too low for this withdrawal")
		}
		requester := requestedBy
		e, err := appendEntry(ctx, tx, groupID, "withdrawal", &requester, -amount, "WD-"+withdrawalID[:8])
		if err != nil {
			return Withdrawal{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE withdrawals SET status='executed', executed_at=now(), ledger_seq=$2 WHERE id=$1`,
			withdrawalID, e.Seq); err != nil {
			return Withdrawal{}, err
		}
		executed = true
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, err
	}
	if executed {
		s.anchorAsync(groupID)
		ref, err := s.mpesa.Payout(ctx, phone, amount, "Chama withdrawal")
		if err != nil {
			s.log.Error("payout failed; needs manual follow-up", "withdrawal", withdrawalID, "err", err)
		} else {
			_, _ = s.db.Exec(ctx, `UPDATE withdrawals SET payout_reference=$2 WHERE id=$1`, withdrawalID, ref)
		}
	}
	return s.GetWithdrawal(ctx, withdrawalID)
}

// RejectWithdrawal records a "no" vote. Once enough members reject that the required number of
// approvals can no longer be reached, the withdrawal is closed and its funds are released.
func (s *Service) RejectWithdrawal(ctx context.Context, withdrawalID, memberID, channel string) (Withdrawal, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Withdrawal{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var groupID, requestedBy, status string
	var required int
	err = tx.QueryRow(ctx, `SELECT group_id, requested_by, status, required_approvals FROM withdrawals WHERE id=$1 FOR UPDATE`,
		withdrawalID).Scan(&groupID, &requestedBy, &status, &required)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return Withdrawal{}, fmt.Errorf("%w: withdrawal", ErrNotFound)
	}
	if err != nil {
		return Withdrawal{}, err
	}
	if status != "pending" {
		return Withdrawal{}, fmt.Errorf("%w: withdrawal is already %s", ErrConflict, status)
	}
	voter, err := s.getMember(ctx, tx, memberID)
	if err != nil {
		return Withdrawal{}, err
	}
	if voter.GroupID != groupID || !voter.Active {
		return Withdrawal{}, fmt.Errorf("%w: voter is not an active member of this group", ErrForbidden)
	}
	if memberID == requestedBy {
		return Withdrawal{}, fmt.Errorf("%w: use cancel to withdraw your own request", ErrForbidden)
	}
	_, err = tx.Exec(ctx, `INSERT INTO withdrawal_approvals(withdrawal_id, member_id, decision, channel) VALUES ($1,$2,'reject',$3)`, withdrawalID, memberID, validChannel(channel))
	if isUniqueViolation(err) {
		return Withdrawal{}, fmt.Errorf("%w: you already voted on this withdrawal", ErrConflict)
	}
	if err != nil {
		return Withdrawal{}, err
	}
	var eligible, rejects int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM members WHERE group_id=$1 AND active AND id<>$2`, groupID, requestedBy).Scan(&eligible); err != nil {
		return Withdrawal{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM withdrawal_approvals WHERE withdrawal_id=$1 AND decision='reject'`, withdrawalID).Scan(&rejects); err != nil {
		return Withdrawal{}, err
	}
	if eligible-rejects < required {
		if _, err := tx.Exec(ctx, `UPDATE withdrawals SET status='rejected' WHERE id=$1`, withdrawalID); err != nil {
			return Withdrawal{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, err
	}
	return s.GetWithdrawal(ctx, withdrawalID)
}

func (s *Service) CancelWithdrawal(ctx context.Context, withdrawalID, memberID string) (Withdrawal, error) {
	tag, err := s.db.Exec(ctx, `UPDATE withdrawals SET status='cancelled' WHERE id=$1 AND requested_by=$2 AND status='pending'`,
		withdrawalID, memberID)
	if isInvalidUUID(err) {
		return Withdrawal{}, fmt.Errorf("%w: withdrawal", ErrNotFound)
	}
	if err != nil {
		return Withdrawal{}, err
	}
	if tag.RowsAffected() == 0 {
		return Withdrawal{}, fmt.Errorf("%w: only the requester can cancel a pending withdrawal", ErrForbidden)
	}
	return s.GetWithdrawal(ctx, withdrawalID)
}

const withdrawalCols = `w.id, w.group_id, w.requested_by, w.amount, w.recipient_phone, w.reason, w.status, w.required_approvals,
	w.risk_score, w.risk_level, w.risk_reasons, w.payout_reference, w.ledger_seq, w.created_at, w.executed_at,
	COALESCE((SELECT array_agg(a.member_id::text ORDER BY a.created_at) FROM withdrawal_approvals a WHERE a.withdrawal_id=w.id AND a.decision='approve'), '{}'),
	COALESCE((SELECT array_agg(a.member_id::text ORDER BY a.created_at) FROM withdrawal_approvals a WHERE a.withdrawal_id=w.id AND a.decision='reject'), '{}')`

func scanWithdrawal(row pgx.Row) (Withdrawal, error) {
	var w Withdrawal
	var reasons []byte
	err := row.Scan(&w.ID, &w.GroupID, &w.RequestedBy, &w.Amount, &w.RecipientPhone, &w.Reason, &w.Status, &w.RequiredApprovals,
		&w.RiskScore, &w.RiskLevel, &reasons, &w.PayoutReference, &w.LedgerSeq, &w.CreatedAt, &w.ExecutedAt, &w.Approvals, &w.Rejections)
	if err != nil {
		return w, err
	}
	_ = json.Unmarshal(reasons, &w.RiskReasons)
	if w.RiskReasons == nil {
		w.RiskReasons = []string{}
	}
	return w, nil
}

func (s *Service) GetWithdrawal(ctx context.Context, id string) (Withdrawal, error) {
	w, err := scanWithdrawal(s.db.QueryRow(ctx, `SELECT `+withdrawalCols+` FROM withdrawals w WHERE w.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return w, fmt.Errorf("%w: withdrawal", ErrNotFound)
	}
	return w, err
}

func (s *Service) ListWithdrawals(ctx context.Context, groupID, status string) ([]Withdrawal, error) {
	if _, err := s.getGroup(ctx, s.db, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+withdrawalCols+` FROM withdrawals w
		WHERE w.group_id=$1 AND ($2='' OR w.status=$2) ORDER BY w.created_at DESC LIMIT 200`, groupID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Withdrawal{}
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// PendingForApprover lists pending withdrawals a member can still approve.
func (s *Service) PendingForApprover(ctx context.Context, memberID string) ([]Withdrawal, error) {
	rows, err := s.db.Query(ctx, `SELECT `+withdrawalCols+` FROM withdrawals w
		JOIN members m ON m.id=$1 AND m.group_id=w.group_id
		WHERE w.status='pending' AND w.requested_by<>$1
		  AND NOT EXISTS (SELECT 1 FROM withdrawal_approvals a WHERE a.withdrawal_id=w.id AND a.member_id=$1)
		ORDER BY w.created_at`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Withdrawal{}
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ---------- blockchain anchoring ----------

func (s *Service) anchorAsync(groupID string) {
	if s.anchorer == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := s.anchorLatest(ctx, groupID); err != nil {
			s.log.Error("anchoring failed (will retry on next entry)", "group", groupID, "err", err)
		}
	}()
}

// anchorLatest anchors the group's current head if it is newer than the last anchor.
// Reading the head at send time keeps anchors in order even when jobs overlap.
func (s *Service) anchorLatest(ctx context.Context, groupID string) error {
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	var seq int64
	var head string
	if err := s.db.QueryRow(ctx, `SELECT seq, hash FROM ledger_entries WHERE group_id=$1 ORDER BY seq DESC LIMIT 1`, groupID).
		Scan(&seq, &head); err != nil {
		return err
	}
	var anchored int64
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(MAX(entries),0) FROM anchors WHERE group_id=$1`, groupID).Scan(&anchored); err != nil {
		return err
	}
	if seq <= anchored {
		return nil
	}
	txHash, err := s.anchorer.Anchor(ctx, groupID, head, uint64(seq))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO anchors(group_id, entries, head, tx_hash) VALUES ($1,$2,$3,$4)`, groupID, seq, head, txHash)
	if err == nil {
		s.log.Info("ledger anchored on-chain", "group", groupID, "entries", seq, "tx", txHash)
	}
	return err
}

// ---------- helpers ----------

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isInvalidUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
