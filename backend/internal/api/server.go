// Package api exposes the HTTP JSON API.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chamachain/backend/internal/core"
	"github.com/chamachain/backend/internal/mpesa"
	"github.com/chamachain/backend/internal/ussd"
)

type Server struct {
	svc        *core.Service
	ussd       *ussd.Handler
	apiKey     string
	trustProxy bool
	log        *slog.Logger

	loginLimit  *limiter // admin sign-in attempts per IP
	otpLimit    *limiter // SMS code requests per IP
	verifyLimit *limiter // SMS code checks per IP
}

func NewServer(svc *core.Service, apiKey string, trustProxy bool, log *slog.Logger) *Server {
	return &Server{
		svc: svc, ussd: ussd.New(svc), apiKey: apiKey, trustProxy: trustProxy, log: log,
		loginLimit:  newLimiter(10, time.Minute),
		otpLimit:    newLimiter(5, time.Minute),
		verifyLimit: newLimiter(15, time.Minute),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /ussd", s.handleUSSD)
	mux.HandleFunc("POST /mpesa/callback", s.handleMpesaCallback)
	// Operations console (the page itself is public; every data call it makes needs a signed-in admin)
	mux.HandleFunc("GET /{$}", serveConsole)
	// Member web app
	s.memberAppRoutes(mux)

	// Sign-in
	mux.HandleFunc("GET /admin/status", s.adminStatus)
	mux.HandleFunc("POST /admin/login", s.limit(s.loginLimit, s.adminLogin))
	mux.HandleFunc("POST /admin/logout", s.adminLogout)
	mux.HandleFunc("POST /auth/otp/request", s.limit(s.otpLimit, s.otpRequest))
	mux.HandleFunc("POST /auth/otp/verify", s.limit(s.verifyLimit, s.otpVerify))

	// Member app (Authorization: Bearer <token>)
	members := s.requireMember(s.memberRoutes())
	mux.Handle("/me", members)
	mux.Handle("/me/", members)

	// Admin endpoints (signed-in admin, or X-API-Key for scripts)
	protected := http.NewServeMux()
	protected.HandleFunc("GET /admin/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, actorFrom(r))
	})
	protected.HandleFunc("GET /audit", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.svc.AuditLog(r.Context(), 200)
		s.respond(w, http.StatusOK, list, err)
	})
	protected.HandleFunc("GET /system", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.svc.SystemStatus(r.Context()))
	})
	protected.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		st, err := s.svc.Stats(r.Context())
		s.respond(w, http.StatusOK, st, err)
	})
	protected.HandleFunc("GET /groups", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.svc.ListGroups(r.Context())
		s.respond(w, http.StatusOK, list, err)
	})
	protected.HandleFunc("GET /groups/{id}/anchors", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.svc.Anchors(r.Context(), r.PathValue("id"))
		s.respond(w, http.StatusOK, list, err)
	})
	protected.HandleFunc("POST /groups", s.createGroup)
	protected.HandleFunc("GET /groups/{id}", s.getGroup)
	protected.HandleFunc("POST /groups/{id}/members", s.addMember)
	protected.HandleFunc("POST /groups/{id}/contributions", s.recordContribution)
	protected.HandleFunc("POST /groups/{id}/contributions/mpesa", s.startMpesaContribution)
	protected.HandleFunc("GET /groups/{id}/ledger", s.getLedger)
	protected.HandleFunc("GET /groups/{id}/ledger/verify", s.verifyLedger)
	protected.HandleFunc("POST /groups/{id}/withdrawals", s.requestWithdrawal)
	protected.HandleFunc("GET /groups/{id}/withdrawals", s.listWithdrawals)
	protected.HandleFunc("GET /withdrawals/{id}", s.getWithdrawal)
	protected.HandleFunc("POST /withdrawals/{id}/approve", s.approveWithdrawal)
	protected.HandleFunc("POST /withdrawals/{id}/reject", s.rejectWithdrawal)
	protected.HandleFunc("POST /withdrawals/{id}/cancel", s.cancelWithdrawal)
	protected.HandleFunc("GET /members/{id}/pending-approvals", s.pendingApprovals)
	mux.Handle("/", s.requireAdmin(protected))

	return s.recoverer(s.logger(securityHeaders(mux)))
}

// ---------- handlers ----------

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name              string `json:"name"`
		Currency          string `json:"currency"`
		ApprovalThreshold int    `json:"approval_threshold"`
	}
	if !decode(w, r, &in) {
		return
	}
	g, err := s.svc.CreateGroup(r.Context(), in.Name, in.Currency, in.ApprovalThreshold)
	s.respond(w, http.StatusCreated, g, err)
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	g, err := s.svc.GetGroup(r.Context(), r.PathValue("id"))
	s.respond(w, http.StatusOK, g, err)
}

func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.svc.AddMember(r.Context(), r.PathValue("id"), in.Name, in.Phone, in.Role)
	s.respond(w, http.StatusCreated, m, err)
}

func (s *Server) recordContribution(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemberID  string `json:"member_id"`
		Amount    int64  `json:"amount"`
		Reference string `json:"reference"`
	}
	if !decode(w, r, &in) {
		return
	}
	e, err := s.svc.RecordContribution(r.Context(), r.PathValue("id"), in.MemberID, in.Amount, in.Reference)
	s.respond(w, http.StatusCreated, e, err)
}

func (s *Server) startMpesaContribution(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemberID string `json:"member_id"`
		Amount   int64  `json:"amount"`
	}
	if !decode(w, r, &in) {
		return
	}
	id, testMode, err := s.svc.StartMpesaContribution(r.Context(), r.PathValue("id"), in.MemberID, in.Amount)
	msg := "Check your phone and enter your M-Pesa PIN to complete the contribution."
	if testMode {
		msg = "Test payment mode: confirmed instantly. Add Safaricom keys to .env to take live M-Pesa payments."
	}
	s.respond(w, http.StatusAccepted, map[string]any{"checkout_request_id": id, "test_mode": testMode, "message": msg}, err)
}

func (s *Server) getLedger(w http.ResponseWriter, r *http.Request) {
	l, err := s.svc.Ledger(r.Context(), r.PathValue("id"))
	s.respond(w, http.StatusOK, l, err)
}

func (s *Server) verifyLedger(w http.ResponseWriter, r *http.Request) {
	rep, err := s.svc.VerifyLedger(r.Context(), r.PathValue("id"))
	s.respond(w, http.StatusOK, rep, err)
}

func (s *Server) requestWithdrawal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RequestedBy    string `json:"requested_by"`
		Amount         int64  `json:"amount"`
		RecipientPhone string `json:"recipient_phone"`
		Reason         string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	wd, err := s.svc.RequestWithdrawal(r.Context(), r.PathValue("id"), in.RequestedBy, in.Amount, in.RecipientPhone, in.Reason)
	s.respond(w, http.StatusCreated, wd, err)
}

func (s *Server) listWithdrawals(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListWithdrawals(r.Context(), r.PathValue("id"), r.URL.Query().Get("status"))
	s.respond(w, http.StatusOK, list, err)
}

func (s *Server) getWithdrawal(w http.ResponseWriter, r *http.Request) {
	wd, err := s.svc.GetWithdrawal(r.Context(), r.PathValue("id"))
	s.respond(w, http.StatusOK, wd, err)
}

func (s *Server) approveWithdrawal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemberID string `json:"member_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	wd, err := s.svc.ApproveWithdrawal(r.Context(), r.PathValue("id"), in.MemberID, voteChannel(r))
	s.respond(w, http.StatusOK, wd, err)
}

func (s *Server) rejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemberID string `json:"member_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	wd, err := s.svc.RejectWithdrawal(r.Context(), r.PathValue("id"), in.MemberID, voteChannel(r))
	s.respond(w, http.StatusOK, wd, err)
}

func (s *Server) cancelWithdrawal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemberID string `json:"member_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	wd, err := s.svc.CancelWithdrawal(r.Context(), r.PathValue("id"), in.MemberID)
	s.respond(w, http.StatusOK, wd, err)
}

func (s *Server) pendingApprovals(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.PendingForApprover(r.Context(), r.PathValue("id"))
	s.respond(w, http.StatusOK, list, err)
}

func (s *Server) handleUSSD(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "END Bad request", http.StatusBadRequest)
		return
	}
	reply := s.ussd.Respond(r.Context(), r.FormValue("phoneNumber"), r.FormValue("text"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, reply)
}

func (s *Server) handleMpesaCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ResultCode": 1, "ResultDesc": "bad request"})
		return
	}
	cb, err := mpesa.ParseCallback(body)
	if err == nil {
		err = s.svc.HandleMpesaCallback(r.Context(), cb)
	}
	if err != nil {
		s.log.Warn("mpesa callback not applied", "err", err)
	}
	// Always acknowledge so Safaricom does not retry forever; problems are logged.
	writeJSON(w, http.StatusOK, map[string]any{"ResultCode": 0, "ResultDesc": "Accepted"})
}

// ---------- helpers ----------

func (s *Server) respond(w http.ResponseWriter, okStatus int, v any, err error) {
	if err == nil {
		writeJSON(w, okStatus, v)
		return
	}
	status, msg := errStatus(err)
	if status == http.StatusInternalServerError {
		s.log.Error("request failed", "err", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func isErr(err, target error) bool { return errors.Is(err, target) }

// voteChannel records whether an official entered the vote in the console or a script sent it.
func voteChannel(r *http.Request) string {
	if actorFrom(r).Type == "admin" {
		return "console"
	}
	return "api"
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// The console polls for live data; only log changes and errors to keep logs readable.
		if r.Method == http.MethodGet && rec.status < 400 {
			return
		}
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "value", v, "path", r.URL.Path)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/ussd") {
			h.Del("Cache-Control")
		}
		next.ServeHTTP(w, r)
	})
}
