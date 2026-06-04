package config

import (
	"errors"
	"os"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	DatabaseURL        string
	JWTSecret          string
	JWTRefreshSecret   string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	R2AccountID        string
	R2AccessKeyID      string
	R2SecretAccessKey  string
	R2Bucket           string
	Port               string
	WebOrigin          string
}

// Load reads configuration from environment variables.
// Returns an error if any required variable is missing.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		JWTRefreshSecret:   os.Getenv("JWT_REFRESH_SECRET"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		R2AccountID:        os.Getenv("R2_ACCOUNT_ID"),
		R2AccessKeyID:      os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey:  os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2Bucket:           os.Getenv("R2_BUCKET"),
		Port:               os.Getenv("PORT"),
		WebOrigin:          os.Getenv("WEB_ORIGIN"),
	}

	if cfg.Port == "" {
		cfg.Port = ":8080"
	} else if cfg.Port[0] != ':' {
		cfg.Port = ":" + cfg.Port
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if cfg.JWTRefreshSecret == "" {
		missing = append(missing, "JWT_REFRESH_SECRET")
	}
	if cfg.GoogleClientID == "" {
		missing = append(missing, "GOOGLE_CLIENT_ID")
	}
	if cfg.GoogleClientSecret == "" {
		missing = append(missing, "GOOGLE_CLIENT_SECRET")
	}
	if cfg.GoogleRedirectURL == "" {
		missing = append(missing, "GOOGLE_REDIRECT_URL")
	}

	if len(missing) > 0 {
		return nil, errors.New("missing required environment variables: " + joinStrings(missing))
	}

	return cfg, nil
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
