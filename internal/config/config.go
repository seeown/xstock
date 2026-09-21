// Package config loads runtime configuration from the project-root
// .env.produce file so the server and the bulk tools share one source.
package config

import (
	"fmt"
	"net/url"
	"os"

	"github.com/joho/godotenv"
)

// EnvFile is the project-root configuration file holding the database
// credentials; nothing connection-related is hardcoded in the binaries.
const EnvFile = ".env.produce"

// LoadEnv reads EnvFile if present; real environment variables always win.
func LoadEnv() error {
	return godotenv.Load(EnvFile)
}

// PGDSN builds the PostgreSQL connection string from the environment.
// Every variable is required so that no credentials or host defaults live
// in the source code.
func PGDSN() (string, error) {
	vals := map[string]string{
		"PG_USER":   os.Getenv("PG_USER"),
		"PG_PASSWD": os.Getenv("PG_PASSWD"),
		"PG_HOST":   os.Getenv("PG_HOST"),
		"PG_PORT":   os.Getenv("PG_PORT"),
		"DB_NAME":   os.Getenv("DB_NAME"),
	}
	for _, key := range []string{"PG_USER", "PG_PASSWD", "PG_HOST", "PG_PORT", "DB_NAME"} {
		if vals[key] == "" {
			return "", fmt.Errorf("%s is not set — check %s", key, EnvFile)
		}
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		url.PathEscape(vals["PG_USER"]), url.PathEscape(vals["PG_PASSWD"]),
		vals["PG_HOST"], vals["PG_PORT"], vals["DB_NAME"]), nil
}
