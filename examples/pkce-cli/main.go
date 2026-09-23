// pkce-cli is a minimal CLI that demonstrates the PKCE browser login against a
// Shopware instance and then calls /_info/me.
//
// Usage:
//
//	./pkce-cli -url https://my-shop.example.com
//
// The session is saved per shop, so subsequent runs skip the browser and
// refresh the access token automatically.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/shopwareLabs/go-shopware-http-client/pkce"
)

func run() error {
	defaultDir, err := pkce.DefaultFileStoreDir()
	if err != nil {
		return err
	}

	baseURL := flag.String("url", "", "Shopware instance URL (required, e.g. https://shop.example.com)")
	sessionDir := flag.String("session-dir", defaultDir, "Directory the login sessions are saved in")
	clientID := flag.String("client-id", "shopware-cli", "OAuth public client ID")
	logout := flag.Bool("logout", false, "Forget the saved session for -url and exit")
	flag.Parse()

	if *baseURL == "" {
		flag.Usage()
		return fmt.Errorf("-url is required")
	}

	ctx := context.Background()

	store, err := pkce.NewFileStore(*sessionDir)
	if err != nil {
		return err
	}

	if *logout {
		return store.Delete(ctx, *baseURL)
	}

	client, err := pkce.NewClient(ctx, pkce.Config{BaseURL: *baseURL, ClientID: *clientID}, store)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	resp, err := client.Get(ctx, "/_info/me")
	if err != nil {
		return fmt.Errorf("api call: %w", err)
	}

	var me map[string]any
	if err := json.Unmarshal(resp.Body, &me); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	pretty, _ := json.MarshalIndent(me, "", "  ")
	fmt.Println(string(pretty))
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
