// Package extension provides a typed helper over the Shopware
// /api/_action/extension/* endpoints: listing, lifecycle (install, update,
// activate, ...), and uploading extension zips.
//
// It is a thin domain layer on top of *shopware.Client and lives in its own
// package so the generic client stays dependency-free — the version-constraint
// library used to gate version-specific behavior is only pulled in when you
// actually use this package.
package extension

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"

	shopware "github.com/shopwareLabs/go-shopware-http-client"
	"github.com/shyim/go-version"
)

// removeUsesPostConstraint is the first version where the remove action is a
// POST instead of a DELETE.
var removeUsesPostConstraint = version.MustConstraints(version.NewConstraint(">=6.6.10.2"))

// Manager calls the extension-manager endpoints of a shop.
type Manager struct {
	client *shopware.Client
}

// NewManager returns an extension Manager bound to the client.
func NewManager(client *shopware.Client) *Manager {
	return &Manager{client: client}
}

// Refresh tells Shopware to re-scan the extension directory.
func (m *Manager) Refresh(ctx context.Context) error {
	_, err := m.client.Post(ctx, "/_action/extension/refresh", nil)
	return err
}

// ListAvailable returns all installed/available extensions.
func (m *Manager) ListAvailable(ctx context.Context) (List, error) {
	resp, err := m.client.Get(ctx, "/_action/extension/installed")
	if err != nil {
		return nil, err
	}
	var list List
	if err := resp.JSON(&list); err != nil {
		return nil, err
	}
	return list, nil
}

// Install installs an extension by type ("plugin" or "app") and technical name.
func (m *Manager) Install(ctx context.Context, extType, name string) error {
	return m.lifecycle(ctx, "POST", fmt.Sprintf("/_action/extension/install/%s/%s", extType, name))
}

// Uninstall uninstalls an extension.
func (m *Manager) Uninstall(ctx context.Context, extType, name string) error {
	return m.lifecycle(ctx, "POST", fmt.Sprintf("/_action/extension/uninstall/%s/%s", extType, name))
}

// Update updates an extension to its latest available version.
func (m *Manager) Update(ctx context.Context, extType, name string) error {
	return m.lifecycle(ctx, "POST", fmt.Sprintf("/_action/extension/update/%s/%s", extType, name))
}

// Download downloads an extension from the store.
func (m *Manager) Download(ctx context.Context, name string) error {
	return m.lifecycle(ctx, "POST", fmt.Sprintf("/_action/extension/download/%s", name))
}

// Activate activates an installed extension.
func (m *Manager) Activate(ctx context.Context, extType, name string) error {
	return m.lifecycle(ctx, "PUT", fmt.Sprintf("/_action/extension/activate/%s/%s", extType, name))
}

// Deactivate deactivates an active extension.
func (m *Manager) Deactivate(ctx context.Context, extType, name string) error {
	return m.lifecycle(ctx, "PUT", fmt.Sprintf("/_action/extension/deactivate/%s/%s", extType, name))
}

// Remove removes an extension. The HTTP method depends on the shop version
// (POST since 6.6.10.2, DELETE before), which is resolved lazily via the
// client's cached Shopware version.
func (m *Manager) Remove(ctx context.Context, extType, name string) error {
	method := "DELETE"
	v, err := m.client.Version(ctx)
	if err != nil {
		return err
	}
	if ver, err := version.NewVersion(v); err == nil && removeUsesPostConstraint.Check(ver) {
		method = "POST"
	}
	return m.lifecycle(ctx, method, fmt.Sprintf("/_action/extension/remove/%s/%s", extType, name))
}

func (m *Manager) lifecycle(ctx context.Context, method, path string) error {
	_, err := m.client.Request(ctx, method, path, nil, nil)
	return err
}

// Upload uploads an extension zip to a self-managed shop.
func (m *Manager) Upload(ctx context.Context, zip io.Reader) error {
	return m.uploadMultipart(ctx, "/_action/extension/upload", nil, zip)
}

// UploadUpdateToCloud uploads an extension update to a cloud shop, associating
// it with the given extension name.
func (m *Manager) UploadUpdateToCloud(ctx context.Context, extensionName string, zip io.Reader) error {
	return m.uploadMultipart(ctx, "/_action/extension/update-private",
		map[string]string{"media": extensionName}, zip)
}

// uploadMultipart builds a multipart/form-data body with the given form fields
// plus a "file" part holding the zip, and POSTs it. The body is rebuilt per
// attempt so the client's 401 retry can resend it.
func (m *Manager) uploadMultipart(ctx context.Context, path string, fields map[string]string, zip io.Reader) error {
	// Build the multipart body once. A multipart.Writer picks a random boundary,
	// so the Content-Type and the body must come from the same writer — we
	// therefore materialize the bytes here and replay them on retry rather than
	// rebuilding (which would mint a new, mismatched boundary).
	var buf bytes.Buffer
	parts := multipart.NewWriter(&buf)

	for name, value := range fields {
		if err := parts.WriteField(name, value); err != nil {
			return err
		}
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="extension.zip"`)
	header.Set("Content-Type", "application/zip")
	part, err := parts.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, zip); err != nil {
		return fmt.Errorf("read extension zip: %w", err)
	}
	if err := parts.Close(); err != nil {
		return err
	}

	bodyBytes := buf.Bytes()
	bodyFactory := func() (io.Reader, error) {
		return bytes.NewReader(bodyBytes), nil
	}

	_, err = m.client.RequestRaw(ctx, "POST", path, bodyFactory, map[string]string{
		"Content-Type": parts.FormDataContentType(),
	})
	return err
}
