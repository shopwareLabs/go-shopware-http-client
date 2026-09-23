// Package instance provides helpers for operations on the shop as a whole
// rather than on a single entity, such as clearing its caches.
//
// It is a thin domain layer on top of *shopware.Client.
package instance

import (
	"context"

	shopware "github.com/shopwareLabs/go-shopware-http-client"
)

// Manager calls the instance-wide action endpoints of a shop.
type Manager struct {
	client *shopware.Client
}

// NewManager returns an instance Manager bound to the client.
func NewManager(client *shopware.Client) *Manager {
	return &Manager{client: client}
}

// ClearCache clears the shop's caches (DELETE /api/_action/cache).
func (m *Manager) ClearCache(ctx context.Context) error {
	_, err := m.client.Delete(ctx, "/_action/cache", nil)
	return err
}
