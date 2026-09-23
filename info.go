package shopware

import (
	"context"
	"fmt"
)

// Info is the payload of GET /api/_info/config.
type Info struct {
	Version         string                `json:"version"`
	VersionRevision string                `json:"versionRevision"`
	Bundles         map[string]InfoBundle `json:"bundles"`
}

// InfoBundle lists the administration assets a bundle ships.
type InfoBundle struct {
	CSS []string `json:"css"`
	JS  []string `json:"js"`
}

// HasBundle reports whether the shop has the given administration bundle.
func (i *Info) HasBundle(name string) bool {
	_, ok := i.Bundles[name]
	return ok
}

// Info returns the shop's /api/_info/config, fetching it once and caching the
// result for the lifetime of the Client. Concurrent callers share a single
// fetch.
func (c *Client) Info(ctx context.Context) (*Info, error) {
	c.infoMu.Lock()
	cached := c.info
	c.infoMu.Unlock()
	if cached != nil {
		return cached, nil
	}

	v, err, _ := c.infoGroup.Do("info", func() (any, error) {
		resp, err := c.Get(ctx, "/_info/config")
		if err != nil {
			return nil, err
		}
		var info Info
		if err := resp.JSON(&info); err != nil {
			return nil, fmt.Errorf("decode _info/config: %w", err)
		}
		c.infoMu.Lock()
		c.info = &info
		c.infoMu.Unlock()
		return &info, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Info), nil
}

// ClearCache clears the shop's caches (DELETE /api/_action/cache).
func (c *Client) ClearCache(ctx context.Context) error {
	_, err := c.Delete(ctx, "/_action/cache", nil)
	return err
}
