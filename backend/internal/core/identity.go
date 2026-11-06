package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chamachain/backend/internal/auth"
	"github.com/chamachain/backend/internal/mpesa"
)

var ErrUnauthorized = errors.New("unauthorized")

const (
	otpTTL             = 5 * time.Minute
	otpMaxAttempts     = 5
	otpMaxPer15Minutes = 3
	memberSessionTTL   = 30 * 24 * time.Hour
	adminSessionTTL    = 12 * time.Hour
	adminMaxFailures   = 5
	adminLockout       = 15 * time.Minute
)

// ---------- audit ----------

// Audit records who did what. Failures are logged, never returned, so auditing cannot block service.
func (s *Service) Audit(ctx context.Context, actorType, actor, action, target string, detail map[string]any, ip string) {
	if detail == nil {
		detail = map[string]any{}
	}
	d, _ := json.Marshal(detail)
	if _, err := s.db.Exec(ctx, `INSERT INTO audit_log(actor_type, actor, action, target, detail, ip) VALUES ($1,$2,$3,$4,$5,$6)`,
		actorType, actor, action, target, d, ip); err != nil {
		s.log.Error("audit write failed", "err", err, "action", action)
	}
}

type AuditEntry struct {
	ActorType string          `json:"actor_type"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Detail    json.RawMessage `json:"detail"`
	IP        string          `json:"ip"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Service) AuditLog(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `SELECT actor_type, actor, action, target, detail, ip, created_at FROM audit_log ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ActorType, &a.Actor, &a.Action, &a.Target, &a.Detail, &a.IP, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- member phone login ----------

// RequestOTP sends a login code if the phone belongs to an active member. To avoid revealing
// which numbers are members, callers always get the same response.
func (s *Service) RequestOTP(ctx context.Context, phone string) error {
	p, err := mpesa.NormalizePhone(phone)
	if err != nil {
		return invalid("enter a valid phone number")
	}
	var isMember bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM members WHERE phone=$1 AND active)`, p).Scan(&isMember); err != nil {
		return err
	}
	if !isMember {
		s.log.Info("login code requested for a number that is not a member")
		return nil
	}
	var recent int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM otp_codes WHERE phone=$1 AND created_at > now() - interval '15 minutes'`, p).Scan(&recent); err != nil {
		return err
	}
	if recent >= otpMaxPer15Minutes {
		return fmt.Errorf("%w: too many codes requested; wait 15 minutes and try again", ErrConflict)
	}
	code, err := auth.NewOTP()
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO otp_codes(phone, code_hash, expires_at) VALUES ($1,$2,$3)`,
		p, auth.HashOTP(s.secret, p, code), time.Now().Add(otpTTL)); err != nil {
		return err
	}
	return s.sms.Send(ctx, p, fmt.Sprintf("Your ChamaChain code is %s. It expires in 5 minutes. Never share this code with anyone, including group officials.", code))
}

// VerifyOTP checks the newest unused code for the phone and starts a member session.
func (s *Service) VerifyOTP(ctx context.Context, phone, code, device string) (string, error) {
	p, err := mpesa.NormalizePhone(phone)
	if err != nil {
		return "", invalid("enter a valid phone number")
	}
	code = strings.TrimSpace(code)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id int64
	var hash string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id, code_hash, attempts FROM otp_codes
		WHERE phone=$1 AND NOT used AND expires_at > now() ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, p).
		Scan(&id, &hash, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: the code has expired; request a new one", ErrUnauthorized)
	}
	if err != nil {
		return "", err
	}
	if attempts >= otpMaxAttempts {
		return "", fmt.Errorf("%w: too many wrong attempts; request a new code", ErrUnauthorized)
	}
	if !auth.EqualHex(hash, auth.HashOTP(s.secret, p, code)) {
		if _, err := tx.Exec(ctx, `UPDATE otp_codes SET attempts=attempts+1 WHERE id=$1`, id); err != nil {
			return "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: wrong code", ErrUnauthorized)
	}
	if _, err := tx.Exec(ctx, `UPDATE otp_codes SET used=TRUE WHERE id=$1`, id); err != nil {
		return "", err
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	if len(device) > 120 {
		device = device[:120]
	}
	if _, err := tx.Exec(ctx, `INSERT INTO member_sessions(token_hash, phone, device, expires_at) VALUES ($1,$2,$3,$4)`,
		tokenHash, p, device, time.Now().Add(memberSessionTTL)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return token, nil
}

// MemberSession returns the phone number behind a member token.
func (s *Service) MemberSession(ctx context.Context, token string) (string, error) {
	var phone string
	err := s.db.QueryRow(ctx, `UPDATE member_sessions SET last_seen_at=now()
		WHERE token_hash=$1 AND expires_at > now() RETURNING phone`, auth.HashToken(token)).Scan(&phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUnauthorized
	}
	return phone, err
}

func (s *Service) EndMemberSession(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM member_sessions WHERE token_hash=$1`, auth.HashToken(token))
	return err
}

// ---------- the member's own view ----------

type MyGroup struct {
	GroupID           string `json:"group_id"`
	GroupName         string `json:"group_name"`
	Currency          string `json:"currency"`
	MemberID          string `json:"member_id"`
	Name              string `json:"name"`
	Role              string `json:"role"`
	Balance           int64  `json:"balance"`
	Available         int64  `json:"available"`
	MyContributions   int64  `json:"my_contributions"`
	ApprovalThreshold int    `json:"approval_threshold"`
	PendingForMe      int    `json:"pending_for_me"`
}

func (s *Service) MyGroups(ctx context.Context, phone string) ([]MyGroup, error) {
	rows, err := s.db.Query(ctx, `SELECT g.id, g.name, g.currency, m.id, m.name, m.role, g.approval_threshold,
		COALESCE((SELECT SUM(amount) FROM ledger_entries l WHERE l.group_id=g.id),0),
		COALESCE((SELECT SUM(amount) FROM withdrawals w WHERE w.group_id=g.id AND w.status='pending'),0),
		COALESCE((SELECT SUM(amount) FROM ledger_entries l WHERE l.member_id=m.id AND l.entry_type='contribution'),0),
		(SELECT COUNT(*) FROM withdrawals w WHERE w.group_id=g.id AND w.status='pending' AND w.requested_by<>m.id
			AND NOT EXISTS (SELECT 1 FROM withdrawal_approvals a WHERE a.withdrawal_id=w.id AND a.member_id=m.id))
		FROM members m JOIN groups g ON g.id=m.group_id
		WHERE m.phone=$1 AND m.active ORDER BY g.name`, phone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MyGroup{}
	for rows.Next() {
		var g MyGroup
		var pending int64
		if err := rows.Scan(&g.GroupID, &g.GroupName, &g.Currency, &g.MemberID, &g.Name, &g.Role, &g.ApprovalThreshold,
			&g.Balance, &pending, &g.MyContributions, &g.PendingForMe); err != nil {
			return nil, err
		}
		g.Available = g.Balance - pending
		out = append(out, g)
	}
	return out, rows.Err()
}

// MemberIn returns the caller's member record in a group, or ErrForbidden.
func (s *Service) MemberIn(ctx context.Context, phone, groupID string) (Member, error) {
	var m Member
	err := s.db.QueryRow(ctx, `SELECT id, group_id, name, phone, role, active, joined_at FROM members
		WHERE phone=$1 AND group_id=$2 AND active`, phone, groupID).
		Scan(&m.ID, &m.GroupID, &m.Name, &m.Phone, &m.Role, &m.Active, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return m, fmt.Errorf("%w: you are not a member of this group", ErrForbidden)
	}
	return m, err
}

// MemberForWithdrawal returns the caller's member record in the withdrawal's group.
func (s *Service) MemberForWithdrawal(ctx context.Context, phone, withdrawalID string) (Member, error) {
	var groupID string
	err := s.db.QueryRow(ctx, `SELECT group_id FROM withdrawals WHERE id=$1`, withdrawalID).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return Member{}, fmt.Errorf("%w: withdrawal", ErrNotFound)
	}
	if err != nil {
		return Member{}, err
	}
	return s.MemberIn(ctx, phone, groupID)
}

type MyApproval struct {
	Withdrawal
	GroupName     string `json:"group_name"`
	Currency      string `json:"currency"`
	RequesterName string `json:"requester_name"`
	RequesterRole string `json:"requester_role"`
	MyMemberID    string `json:"my_member_id"`
}

// MyPendingApprovals lists requests across all the caller's groups that they can still vote on.
func (s *Service) MyPendingApprovals(ctx context.Context, phone string) ([]MyApproval, error) {
	groups, err := s.MyGroups(ctx, phone)
	if err != nil {
		return nil, err
	}
	out := []MyApproval{}
	for _, g := range groups {
		list, err := s.PendingForApprover(ctx, g.MemberID)
		if err != nil {
			return nil, err
		}
		for _, w := range list {
			a := MyApproval{Withdrawal: w, GroupName: g.GroupName, Currency: g.Currency, MyMemberID: g.MemberID}
			if req, err := s.getMember(ctx, s.db, w.RequestedBy); err == nil {
				a.RequesterName, a.RequesterRole = req.Name, req.Role
			}
			out = append(out, a)
		}
	}
	return out, nil
}

type MyActivity struct {
	Seq       int64     `json:"seq"`
	Type      string    `json:"type"`
	Amount    int64     `json:"amount"`
	Mine      bool      `json:"mine"`
	Who       string    `json:"who"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
}

// GroupActivity is the member-facing ledger: amounts and names, newest first.
func (s *Service) GroupActivity(ctx context.Context, groupID, myMemberID string, limit int) ([]MyActivity, error) {
	rows, err := s.db.Query(ctx, `SELECT l.seq, l.entry_type, l.amount, COALESCE(l.member_id::text,''), COALESCE(m.name,''), l.hash, l.created_at
		FROM ledger_entries l LEFT JOIN members m ON m.id=l.member_id
		WHERE l.group_id=$1 ORDER BY l.seq DESC LIMIT $2`, groupID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MyActivity{}
	for rows.Next() {
		var a MyActivity
		var mid string
		if err := rows.Scan(&a.Seq, &a.Type, &a.Amount, &mid, &a.Who, &a.Hash, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Mine = mid == myMemberID
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- admins ----------

type Admin struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func (s *Service) AdminCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

// CreateAdmin stores a new admin and returns the authenticator secret to enrol.
func (s *Service) CreateAdmin(ctx context.Context, username, password string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", invalid("%v", err)
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO admins(username, password_hash, totp_secret) VALUES ($1,$2,$3)`, username, hash, secret)
	if isUniqueViolation(err) {
		return "", fmt.Errorf("%w: that username already exists", ErrConflict)
	}
	if err != nil {
		if strings.Contains(err.Error(), "admins_username_check") {
			return "", invalid("username must be 3-40 characters: lowercase letters, numbers, dot, dash or underscore")
		}
		return "", err
	}
	s.Audit(ctx, "system", "cli", "admin.created", username, nil, "")
	return secret, nil
}

// AdminLogin checks password and authenticator code. Five failures lock the account for 15 minutes.
func (s *Service) AdminLogin(ctx context.Context, username, password, code, ip string) (string, Admin, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	fail := fmt.Errorf("%w: wrong username, password or code", ErrUnauthorized)
	var a Admin
	var hash, secret string
	var lastStep int64
	var failures int
	var lockedUntil *time.Time
	err := s.db.QueryRow(ctx, `SELECT id, username, password_hash, totp_secret, totp_last_step, failed_attempts, locked_until, created_at, last_login_at
		FROM admins WHERE username=$1`, username).
		Scan(&a.ID, &a.Username, &hash, &secret, &lastStep, &failures, &lockedUntil, &a.CreatedAt, &a.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Spend the same time as a real check so usernames cannot be discovered by timing.
		auth.CheckPassword("$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", password)
		s.Audit(ctx, "admin", username, "admin.login_failed", "", map[string]any{"reason": "unknown user"}, ip)
		return "", a, fail
	}
	if err != nil {
		return "", a, err
	}
	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		return "", a, fmt.Errorf("%w: account locked after repeated failures; try again in %d minutes", ErrUnauthorized,
			int(time.Until(*lockedUntil).Minutes())+1)
	}
	passOK := auth.CheckPassword(hash, password)
	step, codeOK := auth.VerifyTOTP(secret, code, time.Now(), lastStep)
	if !passOK || !codeOK {
		failures++
		var lock *time.Time
		if failures >= adminMaxFailures {
			t := time.Now().Add(adminLockout)
			lock, failures = &t, 0
		}
		_, _ = s.db.Exec(ctx, `UPDATE admins SET failed_attempts=$2, locked_until=$3 WHERE id=$1`, a.ID, failures, lock)
		s.Audit(ctx, "admin", username, "admin.login_failed", "", map[string]any{"password_ok": passOK, "code_ok": codeOK}, ip)
		return "", a, fail
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		return "", a, err
	}
	if _, err := s.db.Exec(ctx, `UPDATE admins SET failed_attempts=0, locked_until=NULL, totp_last_step=$2, last_login_at=now() WHERE id=$1`, a.ID, step); err != nil {
		return "", a, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO admin_sessions(token_hash, admin_id, expires_at) VALUES ($1,$2,$3)`,
		tokenHash, a.ID, time.Now().Add(adminSessionTTL)); err != nil {
		return "", a, err
	}
	s.Audit(ctx, "admin", username, "admin.login", "", nil, ip)
	return token, a, nil
}

func (s *Service) AdminSession(ctx context.Context, token string) (Admin, error) {
	var a Admin
	err := s.db.QueryRow(ctx, `SELECT a.id, a.username, a.created_at, a.last_login_at FROM admin_sessions s
		JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=$1 AND s.expires_at > now()`, auth.HashToken(token)).
		Scan(&a.ID, &a.Username, &a.CreatedAt, &a.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrUnauthorized
	}
	return a, err
}

func (s *Service) AdminLogout(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM admin_sessions WHERE token_hash=$1`, auth.HashToken(token))
	return err
}

// PurgeExpired removes expired codes and sessions. Called periodically.
func (s *Service) PurgeExpired(ctx context.Context) {
	_, _ = s.db.Exec(ctx, `DELETE FROM otp_codes WHERE expires_at < now() - interval '1 day'`)
	_, _ = s.db.Exec(ctx, `DELETE FROM member_sessions WHERE expires_at < now()`)
	_, _ = s.db.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at < now()`)
}
