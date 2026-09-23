# PKCE CLI Example

A minimal CLI that demonstrates the PKCE (browser login) flow against a Shopware instance using `pkce.NewClient`.

## What it does

1. **Logs in:** on the first run it opens your browser to the Shopware authorization page, waits for the callback on a local loopback server, and exchanges the code for tokens.
2. **Calls `/_info/me`** to display the current user's information.
3. **Keeps you logged in:** the session is saved per shop, and every token change (including rotated refresh tokens) is written as it happens, so later runs skip the browser.

## Usage

```bash
# First run: opens the browser for login
go run ./examples/pkce-cli -url https://my-shop.example.com

# Later runs: reuse the saved session and refresh automatically if expired
go run ./examples/pkce-cli -url https://my-shop.example.com

# Log out
go run ./examples/pkce-cli -url https://my-shop.example.com -logout
```

## Flags

| Flag           | Default                                     | Description                                             |
|----------------|---------------------------------------------|---------------------------------------------------------|
| `-url`         | (required)                                  | Shopware instance URL (e.g. `https://shop.example.com`) |
| `-client-id`   | `shopware-cli`                              | OAuth public client ID registered in Shopware           |
| `-session-dir` | `<user config dir>/go-shopware-http-client/pkce` | Directory the login sessions are saved in          |
| `-logout`      | `false`                                     | Forget the saved session for `-url` and exit            |

## Prerequisites

The Shopware instance must have a public OAuth client registered with the ID `shopware-cli` (or whatever you pass via `-client-id`). See the [authentication docs](../../docs/authentication.md) for setup instructions.

## Session storage

`pkce.FileStore` keeps one JSON file per shop, named by a hash of the base URL, with file permissions `0600` (owner read/write only).
