// Package ussd serves the basic-phone menu (Africa's Talking USSD format).
// Members dial the service code and can check balances and approve withdrawals without a smartphone.
package ussd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chamachain/backend/internal/core"
)

type Handler struct {
	svc *core.Service
}

func New(svc *core.Service) *Handler { return &Handler{svc: svc} }

// Respond returns the USSD reply. "CON ..." keeps the session open, "END ..." closes it.
// text is the full input history joined by "*", e.g. "3*1*1".
func (h *Handler) Respond(ctx context.Context, phone, text string) string {
	members, err := h.svc.MembersByPhone(ctx, phone)
	if err != nil || len(members) == 0 {
		return "END This number is not registered in any ChamaChain group."
	}
	m := members[0] // MVP: first group; multi-group selection comes later
	g, err := h.svc.GetGroup(ctx, m.GroupID)
	if err != nil {
		return "END Service temporarily unavailable. Try again later."
	}

	parts := []string{}
	if text != "" {
		parts = strings.Split(text, "*")
	}
	if len(parts) == 0 {
		return fmt.Sprintf("CON %s - Hi %s\n1. Group balance\n2. My contributions\n3. Approve withdrawals\n4. Check ledger integrity", g.Name, firstName(m.Name))
	}

	switch parts[0] {
	case "1":
		return fmt.Sprintf("END %s balance: %s %d\nAvailable: %s %d", g.Name, g.Currency, g.Balance, g.Currency, g.Available)
	case "2":
		total, err := h.svc.MemberContributed(ctx, m.ID)
		if err != nil {
			return "END Service temporarily unavailable."
		}
		return fmt.Sprintf("END You have contributed %s %d to %s.", g.Currency, total, g.Name)
	case "3":
		return h.approvals(ctx, m, g, parts[1:])
	case "4":
		rep, err := h.svc.VerifyLedger(ctx, g.ID)
		if err != nil {
			return "END Service temporarily unavailable."
		}
		if rep.Valid {
			return fmt.Sprintf("END Ledger OK: %d records verified, none altered.", rep.Entries)
		}
		return "END WARNING: ledger integrity check FAILED. Contact your group officials now."
	default:
		return "END Invalid choice."
	}
}

func (h *Handler) approvals(ctx context.Context, m core.Member, g core.GroupDetail, rest []string) string {
	pending, err := h.svc.PendingForApprover(ctx, m.ID)
	if err != nil {
		return "END Service temporarily unavailable."
	}
	if len(pending) == 0 {
		return "END No withdrawals waiting for your approval."
	}
	if len(rest) == 0 {
		var b strings.Builder
		b.WriteString("CON Pending withdrawals:\n")
		for i, w := range pending {
			if i == 5 {
				break
			}
			flag := ""
			if w.RiskLevel == "high" {
				flag = " [HIGH RISK]"
			}
			fmt.Fprintf(&b, "%d. %s %d%s\n", i+1, g.Currency, w.Amount, flag)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	idx, err := strconv.Atoi(rest[0])
	if err != nil || idx < 1 || idx > len(pending) || idx > 5 {
		return "END Invalid choice."
	}
	w := pending[idx-1]
	if len(rest) == 1 {
		reason := w.Reason
		if reason == "" {
			reason = "-"
		}
		return fmt.Sprintf("CON %s %d to %s\nReason: %s\nRisk: %s\n1. Approve\n2. Reject", g.Currency, w.Amount, w.RecipientPhone, reason, w.RiskLevel)
	}
	if rest[1] == "2" {
		updated, err := h.svc.RejectWithdrawal(ctx, w.ID, m.ID, "ussd")
		if err != nil {
			return "END Could not reject: " + shortErr(err)
		}
		if updated.Status == "rejected" {
			return "END Rejected. The withdrawal has been stopped."
		}
		return "END Your rejection was recorded."
	}
	if rest[1] != "1" {
		return "END No action taken."
	}
	updated, err := h.svc.ApproveWithdrawal(ctx, w.ID, m.ID, "ussd")
	if err != nil {
		return "END Could not approve: " + shortErr(err)
	}
	if updated.Status == "executed" {
		return "END Approved. Enough approvals received: payment is being sent."
	}
	return fmt.Sprintf("END Approved (%d of %d approvals).", len(updated.Approvals), updated.RequiredApprovals)
}

func firstName(n string) string {
	if f := strings.Fields(n); len(f) > 0 {
		return f[0]
	}
	return n
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
