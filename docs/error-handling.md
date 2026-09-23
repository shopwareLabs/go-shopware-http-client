# Error handling

[← Docs index](./README.md)

Non-2xx responses are returned as `*APIError`. When the body matches Shopware's
error envelope, `Detail` holds the first error's message.

```go
resp, err := client.Get(ctx, "/search/does-not-exist")
if err != nil {
	var apiErr *shopware.APIError
	if errors.As(err, &apiErr) {
		fmt.Println("status:", apiErr.StatusCode)
		fmt.Println("detail:", apiErr.Detail) // e.g. "Entity does-not-exist not found"
		fmt.Println("raw body:", apiErr.Body)
	}
	return err
}
```

On a `401`, the client clears the cached token and retries the request **once**
with a fresh token before surfacing the error, so an expired token does not leak
out as a failure.

## Token errors

Errors from `/api/oauth/token` surface from the request that needed the token
(or from `Authenticate`):

- **Rejected credentials:** a non-2xx response is an `*APIError`, like any other.
- **A response that is not token JSON:** for example, an HTML page from a proxy
  or a wrong base URL. The error quotes the status, content type and the first
  200 bytes of the body:
  `decode token response (status 200, content-type "text/html", body "<!DOCTYPE html>..."): ...`
- **No `access_token` in the response:** reported as `token response (status 200) has no access_token`.
- **A rotated refresh token that could not be saved** (see
  `RefreshTokenCredentials.OnRotate`): reported as `persist rotated refresh token: ...`.

## See also

- [Token storage](./token-storage.md) — how the cached token is invalidated.
