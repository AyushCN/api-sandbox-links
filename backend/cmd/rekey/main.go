// Command rekey re-encrypts all database secrets from OLD_ENCRYPTION_KEY to NEW_ENCRYPTION_KEY.
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/secret"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	oldHex := os.Getenv("OLD_ENCRYPTION_KEY")
	newHex := os.Getenv("NEW_ENCRYPTION_KEY")

	if dbURL == "" || oldHex == "" || newHex == "" {
		log.Fatalf("Usage: DATABASE_URL=... OLD_ENCRYPTION_KEY=<64hex> NEW_ENCRYPTION_KEY=<64hex> go run ./cmd/rekey")
	}

	oldKey, err := hex.DecodeString(oldHex)
	if err != nil || len(oldKey) != 32 {
		log.Fatalf("OLD_ENCRYPTION_KEY must be a 64-character hex string (32 bytes)")
	}
	newKey, err := hex.DecodeString(newHex)
	if err != nil || len(newKey) != 32 {
		log.Fatalf("NEW_ENCRYPTION_KEY must be a 64-character hex string (32 bytes)")
	}

	oldBox, err := secret.NewBox(oldKey)
	if err != nil {
		log.Fatalf("creating old box: %v", err)
	}
	newBox, err := secret.NewBox(newKey)
	if err != nil {
		log.Fatalf("creating new box: %v", err)
	}

	gdb, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}

	err = gdb.Transaction(func(tx *gorm.DB) error {
		// 1. Re-key Users
		var users []db.User
		if err := tx.Find(&users).Error; err != nil {
			return fmt.Errorf("reading users: %w", err)
		}
		for _, u := range users {
			if len(u.GithubToken) == 0 {
				continue
			}
			plain, err := oldBox.Decrypt(u.GithubToken)
			if err != nil {
				return fmt.Errorf("user %s: decrypt token: %w", u.ID, err)
			}
			sealed, err := newBox.Encrypt(plain)
			if err != nil {
				return fmt.Errorf("user %s: encrypt token: %w", u.ID, err)
			}
			if err := tx.Model(&u).Update("github_token", sealed).Error; err != nil {
				return fmt.Errorf("user %s: update token: %w", u.ID, err)
			}
		}
		log.Printf("Re-keyed %d users", len(users))

		// 2. Re-key Sandbox Env Vars
		var envVars []db.EnvVar
		if err := tx.Find(&envVars).Error; err != nil {
			return fmt.Errorf("reading env vars: %w", err)
		}
		for _, ev := range envVars {
			if len(ev.Value) == 0 {
				continue
			}
			plain, err := oldBox.Decrypt(ev.Value)
			if err != nil {
				return fmt.Errorf("env var %s: decrypt value: %w", ev.ID, err)
			}
			sealed, err := newBox.Encrypt(plain)
			if err != nil {
				return fmt.Errorf("env var %s: encrypt value: %w", ev.ID, err)
			}
			if err := tx.Model(&ev).Update("value", sealed).Error; err != nil {
				return fmt.Errorf("env var %s: update value: %w", ev.ID, err)
			}
		}
		log.Printf("Re-keyed %d sandbox env vars", len(envVars))

		// 3. Re-key Sidecar Databases
		var dbs []db.SandboxDatabase
		if err := tx.Find(&dbs).Error; err != nil {
			return fmt.Errorf("reading sandbox databases: %w", err)
		}
		for _, sdb := range dbs {
			if len(sdb.ConnectionURL) == 0 {
				continue
			}
			plain, err := oldBox.Decrypt(sdb.ConnectionURL)
			if err != nil {
				return fmt.Errorf("sandbox database %s: decrypt url: %w", sdb.ID, err)
			}
			sealed, err := newBox.Encrypt(plain)
			if err != nil {
				return fmt.Errorf("sandbox database %s: encrypt url: %w", sdb.ID, err)
			}
			if err := tx.Model(&sdb).Update("connection_url", sealed).Error; err != nil {
				return fmt.Errorf("sandbox database %s: update url: %w", sdb.ID, err)
			}
		}
		log.Printf("Re-keyed %d sandbox databases", len(dbs))

		return nil
	})

	if err != nil {
		log.Fatalf("Re-keying transaction rolled back: %v", err)
	}

	log.Println("SUCCESS: All stored secrets re-encrypted with NEW_ENCRYPTION_KEY.")
}
