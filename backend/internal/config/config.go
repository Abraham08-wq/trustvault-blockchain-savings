// Package config loads settings from environment variables.
package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Port        string
	APIKey      string
	DatabaseURL string
	AIURL       string

	ChainRPCURL     string
	ChainID         int64
	ChainContract   string
	ChainPrivateKey string

	MpesaEnv            string
	MpesaConsumerKey    string
	MpesaConsumerSecret string
	MpesaShortcode      string
	MpesaPasskey        string
	MpesaCallbackURL    string

	SessionSecret string // keys one-time code hashes; at least 32 characters
	TrustProxy    bool   // read client IPs from X-Forwarded-For (only behind your own reverse proxy)

	SMSEnv      string
	SMSUsername string
	SMSAPIKey   string
	SMSSenderID string
}

func Load() (Config, error) {
	c := Config{
		Port:                get("PORT", "8080"),
		APIKey:              os.Getenv("API_KEY"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		AIURL:               get("AI_SERVICE_URL", "http://localhost:8000"),
		ChainRPCURL:         os.Getenv("CHAIN_RPC_URL"),
		ChainContract:       os.Getenv("CHAIN_CONTRACT_ADDRESS"),
		ChainPrivateKey:     os.Getenv("CHAIN_PRIVATE_KEY"),
		MpesaEnv:            get("MPESA_ENV", "sandbox"),
		MpesaConsumerKey:    os.Getenv("MPESA_CONSUMER_KEY"),
		MpesaConsumerSecret: os.Getenv("MPESA_CONSUMER_SECRET"),
		MpesaShortcode:      get("MPESA_SHORTCODE", "174379"),
		MpesaPasskey:        os.Getenv("MPESA_PASSKEY"),
		MpesaCallbackURL:    os.Getenv("MPESA_CALLBACK_URL"),
		SessionSecret:       os.Getenv("SESSION_SECRET"),
		TrustProxy:          os.Getenv("TRUST_PROXY") == "true",
		SMSEnv:              get("AT_ENV", "sandbox"),
		SMSUsername:         os.Getenv("AT_USERNAME"),
		SMSAPIKey:           os.Getenv("AT_API_KEY"),
		SMSSenderID:         os.Getenv("AT_SENDER_ID"),
	}
	id, err := strconv.ParseInt(get("CHAIN_ID", "31337"), 10, 64)
	if err != nil {
		return c, errors.New("CHAIN_ID must be a number")
	}
	c.ChainID = id

	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if len(c.APIKey) < 16 || c.APIKey == "change-me-to-a-long-random-string" {
		return c, errors.New("API_KEY must be set to a random string of at least 16 characters")
	}
	if len(c.SessionSecret) < 32 {
		return c, errors.New("SESSION_SECRET must be a random string of at least 32 characters (run: make setup)")
	}
	return c, nil
}

func (c Config) AnchoringEnabled() bool {
	return c.ChainRPCURL != "" && c.ChainContract != "" && c.ChainPrivateKey != ""
}

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
