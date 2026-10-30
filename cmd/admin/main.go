// Command admin holds operator tools that must not be reachable over HTTP.
//
//	admin promote -email user@example.com   grant platform admin
//	admin demote  -email user@example.com   revoke platform admin
//	admin gen-key                           print a new encryption key
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/envelope"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: admin promote|demote -email <email> | gen-key | gen-recipe-key")
	}
	switch args[0] {
	case "gen-key":
		fmt.Println(envelope.GenerateKey())
		return nil
	case "gen-recipe-key":
		// The seed goes to RECIPE_SIGNING_KEY on the server; the public key
		// is bundled into the extension build (WXT_RECIPE_PUBLIC_KEYS).
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return err
		}
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		fmt.Printf("RECIPE_SIGNING_KEY=%s\nrecipe public key: %s\n", base64.StdEncoding.EncodeToString(seed), base64.StdEncoding.EncodeToString(pub))
		return nil
	case "promote", "demote":
		fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
		email := fs.String("email", "", "user email")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *email == "" {
			return errors.New("-email is required")
		}
		return setAdmin(strings.ToLower(strings.TrimSpace(*email)), args[0] == "promote")
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func setAdmin(email string, admin bool) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	tag, err := conn.Exec(ctx, `UPDATE users SET is_platform_admin = $1 WHERE email = $2 AND deleted_at IS NULL`, admin, email)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no active user with email %s", email)
	}
	fmt.Printf("%s: is_platform_admin = %v\n", email, admin)
	return nil
}
