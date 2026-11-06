package api

import (
	"net/http"

	"github.com/chamachain/backend/internal/core"
)

// Member app endpoints. Every call is scoped to the signed-in phone number: a member can only
// see and act on groups they belong to, and always acts as themselves.

func (s *Server) memberRoutes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /me", s.meHome)
	m.HandleFunc("POST /me/logout", s.meLogout)
	m.HandleFunc("GET /me/groups/{id}", s.meGroup)
	m.HandleFunc("GET /me/groups/{id}/activity", s.meActivity)
	m.HandleFunc("GET /me/groups/{id}/verify", s.meVerify)
	m.HandleFunc("GET /me/groups/{id}/withdrawals", s.meWithdrawals)
	m.HandleFunc("POST /me/groups/{id}/contributions", s.meContribute)
	m.HandleFunc("POST /me/groups/{id}/withdrawals", s.meRequestWithdrawal)
	m.HandleFunc("GET /me/approvals", s.meApprovals)
	m.HandleFunc("POST /me/withdrawals/{id}/approve", s.meVote(true))
	m.HandleFunc("POST /me/withdrawals/{id}/reject", s.meVote(false))
	m.HandleFunc("POST /me/withdrawals/{id}/cancel", s.meCancel)
	return m
}

func (s *Server) meHome(w http.ResponseWriter, r *http.Request) {
	phone := phoneFrom(r)
	groups, err := s.svc.MyGroups(r.Context(), phone)
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	name := ""
	pending := 0
	for _, g := range groups {
		if name == "" {
			name = g.Name
		}
		pending += g.PendingForMe
	}
	writeJSON(w, http.StatusOK, map[string]any{"phone": phone, "name": name, "groups": groups, "pending_approvals": pending})
}

func (s *Server) meLogout(w http.ResponseWriter, r *http.Request) {
	token, viaCookie := memberToken(r)
	err := s.svc.EndMemberSession(r.Context(), token)
	if viaCookie {
		http.SetCookie(w, &http.Cookie{Name: memberCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	s.respond(w, http.StatusOK, map[string]string{"status": "signed out"}, err)
}

func (s *Server) meGroup(w http.ResponseWriter, r *http.Request) {
	me, err := s.svc.MemberIn(r.Context(), phoneFrom(r), r.PathValue("id"))
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	for _, g := range mustGroups(s, r) {
		if g.GroupID == me.GroupID {
			writeJSON(w, http.StatusOK, g)
			return
		}
	}
	s.respond(w, 0, nil, core.ErrNotFound)
}

func mustGroups(s *Server, r *http.Request) []core.MyGroup {
	g, _ := s.svc.MyGroups(r.Context(), phoneFrom(r))
	return g
}

func (s *Server) meActivity(w http.ResponseWriter, r *http.Request) {
	me, err := s.svc.MemberIn(r.Context(), phoneFrom(r), r.PathValue("id"))
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	list, err := s.svc.GroupActivity(r.Context(), me.GroupID, me.ID, 50)
	s.respond(w, http.StatusOK, list, err)
}

func (s *Server) meVerify(w http.ResponseWriter, r *http.Request) {
	me, err := s.svc.MemberIn(r.Context(), phoneFrom(r), r.PathValue("id"))
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	rep, err := s.svc.VerifyLedger(r.Context(), me.GroupID)
	s.respond(w, http.StatusOK, rep, err)
}

func (s *Server) meWithdrawals(w http.ResponseWriter, r *http.Request) {
	me, err := s.svc.MemberIn(r.Context(), phoneFrom(r), r.PathValue("id"))
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	list, err := s.svc.ListWithdrawals(r.Context(), me.GroupID, "")
	s.respond(w, http.StatusOK, list, err)
}

func (s *Server) meContribute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Amount int64 `json:"amount"`
	}
	if !decode(w, r, &in) {
		return
	}
	me, err := s.svc.MemberIn(r.Context(), phoneFrom(r), r.PathValue("id"))
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	id, testMode, err := s.svc.StartMpesaContribution(r.Context(), me.GroupID, me.ID, in.Amount)
	msg := "Check your phone and enter your M-Pesa PIN to complete the contribution."
	if testMode {
		msg = "Test payment mode: your contribution was recorded instantly. No money moved."
	}
	s.respond(w, http.StatusAccepted, map[string]any{"checkout_request_id": id, "test_mode": testMode, "message": msg}, err)
}

func (s *Server) meRequestWithdrawal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Amount         int64  `json:"amount"`
		RecipientPhone string `json:"recipient_phone"`
		Reason         string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	me, err := s.svc.MemberIn(r.Context(), phoneFrom(r), r.PathValue("id"))
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	if in.RecipientPhone == "" {
		in.RecipientPhone = me.Phone
	}
	wd, err := s.svc.RequestWithdrawal(r.Context(), me.GroupID, me.ID, in.Amount, in.RecipientPhone, in.Reason)
	s.respond(w, http.StatusCreated, wd, err)
}

func (s *Server) meApprovals(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.MyPendingApprovals(r.Context(), phoneFrom(r))
	s.respond(w, http.StatusOK, list, err)
}

func (s *Server) meVote(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, err := s.svc.MemberForWithdrawal(r.Context(), phoneFrom(r), r.PathValue("id"))
		if err != nil {
			s.respond(w, 0, nil, err)
			return
		}
		var wd core.Withdrawal
		if approve {
			wd, err = s.svc.ApproveWithdrawal(r.Context(), r.PathValue("id"), me.ID, "app")
		} else {
			wd, err = s.svc.RejectWithdrawal(r.Context(), r.PathValue("id"), me.ID, "app")
		}
		s.respond(w, http.StatusOK, wd, err)
	}
}

func (s *Server) meCancel(w http.ResponseWriter, r *http.Request) {
	me, err := s.svc.MemberForWithdrawal(r.Context(), phoneFrom(r), r.PathValue("id"))
	if err != nil {
		s.respond(w, 0, nil, err)
		return
	}
	wd, err := s.svc.CancelWithdrawal(r.Context(), r.PathValue("id"), me.ID)
	s.respond(w, http.StatusOK, wd, err)
}
