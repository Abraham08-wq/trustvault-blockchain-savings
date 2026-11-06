// Command api runs the ChamaChain backend.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chamachain/backend/internal/api"
	"github.com/chamachain/backend/internal/auth"
	"github.com/chamachain/backend/internal/chain"
	"github.com/chamachain/backend/internal/config"
	"github.com/chamachain/backend/internal/core"
	"github.com/chamachain/backend/internal/mpesa"
	"github.com/chamachain/backend/internal/risk"
	"github.com/chamachain/backend/internal/sms"
	"github.com/chamachain/backend/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 && os.Args[1] == "create-admin" {
		if err := createAdmin(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		return err
	}
	log.Info("database ready")

	var anchorer *chain.Anchorer
	if cfg.AnchoringEnabled() {
		anchorer, err = chain.NewAnchorer(cfg.ChainRPCURL, cfg.ChainID, cfg.ChainContract, cfg.ChainPrivateKey)
		if err != nil {
			return err
		}
		log.Info("blockchain anchoring enabled", "relayer", anchorer.From(), "contract", cfg.ChainContract)
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := anchorer.CheckContract(checkCtx); err != nil {
			log.Error("blockchain check failed; anchoring will be skipped until fixed", "err", err)
		}
		cancel()
	} else {
		log.Warn("blockchain anchoring disabled (set CHAIN_CONTRACT_ADDRESS to enable)")
	}

	mp := mpesa.New(mpesa.Config{
		Env: cfg.MpesaEnv, ConsumerKey: cfg.MpesaConsumerKey, ConsumerSecret: cfg.MpesaConsumerSecret,
		Shortcode: cfg.MpesaShortcode, Passkey: cfg.MpesaPasskey, CallbackURL: cfg.MpesaCallbackURL,
	})
	if mp.TestMode() {
		log.Warn("M-Pesa in test payment mode: add Safaricom keys to .env for live payments")
	}

	sender := sms.New(sms.Config{Env: cfg.SMSEnv, Username: cfg.SMSUsername, APIKey: cfg.SMSAPIKey, SenderID: cfg.SMSSenderID}, log)
	if sender.TestMode() {
		log.Warn("SMS in test mode: login codes are written to this log (add Africa's Talking keys to .env to send real SMS)")
	}

	svc := core.NewService(db, risk.NewClient(cfg.AIURL), mp, anchorer, sender, []byte(cfg.SessionSecret), log)
	go func() {
		for {
			svc.PurgeExpired(context.Background())
			time.Sleep(time.Hour)
		}
	}()
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewServer(svc, cfg.APIKey, cfg.TrustProxy, log).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("ChamaChain API listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	svc.Wait()
	return err
}

// createAdmin is run as: create-admin <username>, with the password on standard input.
// It prints the authenticator secret to add to Google Authenticator, Microsoft Authenticator or similar.
func createAdmin(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: create-admin <username>   (password is read from standard input)")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pw, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	pw = strings.TrimRight(pw, "\r\n")
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		return err
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := core.NewService(db, nil, nil, nil, nil, []byte(cfg.SessionSecret), quiet)
	secret, err := svc.CreateAdmin(ctx, args[0], pw)
	if err != nil {
		return err
	}
	username := strings.ToLower(strings.TrimSpace(args[0]))
	fmt.Println("SECRET=" + secret)
	fmt.Println("URI=" + auth.TOTPURI("ChamaChain Shield", username, secret))
	return nil
}
