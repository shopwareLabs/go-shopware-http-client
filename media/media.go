// Package media provides helpers for working with Shopware media: uploading
// files (by binary content or by URL) and resolving/creating media folders.
//
// It is a thin domain layer on top of *shopware.Client, built from the same
// EntityRepository and Criteria primitives, and lives in its own package so the
// generic client stays focused.
package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	shopware "github.com/shyim/go-shopware-http-client"
)

// Manager performs media operations against a shop.
type Manager struct {
	client *shopware.Client
}

// NewManager returns a media Manager bound to the client.
func NewManager(client *shopware.Client) *Manager {
	return &Manager{client: client}
}

// UploadOptions configures an Upload.
type UploadOptions struct {
	// FileName is the file name including its extension (e.g. "logo.png"). The
	// extension is required.
	FileName string

	// ContentType is the MIME type sent with the binary upload (e.g.
	// "image/png"). If empty, the server infers it from the extension.
	ContentType string

	// Private marks the media as private (not publicly accessible).
	Private bool

	// MediaFolderID optionally places the media in a folder.
	MediaFolderID string
}

// Upload creates a media entity and uploads the binary content of file. It
// returns the new media ID. If the binary upload fails, the just-created media
// entity is rolled back (deleted).
func (m *Manager) Upload(ctx context.Context, file io.Reader, opts UploadOptions) (string, error) {
	baseName, extension, err := splitFileName(opts.FileName)
	if err != nil {
		return "", err
	}

	// Buffer the content so the upload can be retried on a 401.
	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read media file: %w", err)
	}

	mediaID, err := m.createMedia(ctx, opts.Private, opts.MediaFolderID)
	if err != nil {
		return "", err
	}

	query := url.Values{}
	query.Set("extension", extension)
	query.Set("fileName", baseName)
	path := fmt.Sprintf("/_action/media/%s/upload?%s", mediaID, query.Encode())

	headers := map[string]string{}
	if opts.ContentType != "" {
		headers["Content-Type"] = opts.ContentType
	}

	_, err = m.client.RequestRaw(ctx, "POST", path,
		func() (io.Reader, error) { return bytes.NewReader(content), nil }, headers)
	if err != nil {
		m.rollback(ctx, mediaID)
		return "", err
	}
	return mediaID, nil
}

// UploadURLOptions configures an UploadByURL.
type UploadURLOptions struct {
	// FileName is the target file name including its extension.
	FileName string

	// URL is the source the server downloads the media from.
	URL string

	// Private marks the media as private.
	Private bool

	// MediaFolderID optionally places the media in a folder.
	MediaFolderID string
}

// UploadByURL creates a media entity and tells the server to download its
// content from opts.URL. It returns the new media ID, rolling back the media
// entity if the upload request fails.
func (m *Manager) UploadByURL(ctx context.Context, opts UploadURLOptions) (string, error) {
	baseName, extension, err := splitFileName(opts.FileName)
	if err != nil {
		return "", err
	}

	mediaID, err := m.createMedia(ctx, opts.Private, opts.MediaFolderID)
	if err != nil {
		return "", err
	}

	body := map[string]any{
		"fileName":  baseName,
		"extension": extension,
		"url":       opts.URL,
	}
	if _, err := m.client.Post(ctx, fmt.Sprintf("/_action/media/%s/upload", mediaID), body); err != nil {
		m.rollback(ctx, mediaID)
		return "", err
	}
	return mediaID, nil
}

// createMedia upserts a blank media entity and returns its generated ID.
func (m *Manager) createMedia(ctx context.Context, private bool, folderID string) (string, error) {
	mediaID := shopware.UUID()
	entity := map[string]any{
		"id":            mediaID,
		"private":       private,
		"mediaFolderId": nullable(folderID),
	}
	repo := shopware.NewRepository[map[string]any](m.client, "media")
	if err := repo.Upsert(ctx, []map[string]any{entity}); err != nil {
		return "", err
	}
	return mediaID, nil
}

func (m *Manager) rollback(ctx context.Context, mediaID string) {
	repo := shopware.NewRepository[map[string]any](m.client, "media")
	_ = repo.Delete(ctx, []map[string]any{{"id": mediaID}})
}

// DefaultFolderByEntity returns the default media folder ID configured for the
// given entity (e.g. "product"), or "" if none is configured.
func (m *Manager) DefaultFolderByEntity(ctx context.Context, entity string) (string, error) {
	type defaultFolder struct {
		Folder struct {
			ID string `json:"id"`
		} `json:"folder"`
	}

	repo := shopware.NewRepository[defaultFolder](m.client, "media_default_folder")
	criteria := shopware.NewCriteria().
		AddAssociation("folder").
		AddFilter(shopware.Equals("entity", entity)).
		SetLimit(1)

	result, err := repo.Search(ctx, criteria)
	if err != nil {
		return "", err
	}
	if first := result.First(); first != nil {
		return first.Folder.ID, nil
	}
	return "", nil
}

// FolderByName returns the ID of the media folder with the given name, or "" if
// not found.
func (m *Manager) FolderByName(ctx context.Context, name string) (string, error) {
	type folder struct {
		ID string `json:"id"`
	}

	repo := shopware.NewRepository[folder](m.client, "media_folder")
	criteria := shopware.NewCriteria().
		AddFilter(shopware.Equals("name", name)).
		SetLimit(1)

	result, err := repo.Search(ctx, criteria)
	if err != nil {
		return "", err
	}
	if first := result.First(); first != nil {
		return first.ID, nil
	}
	return "", nil
}

// CreateFolderOptions configures CreateFolder.
type CreateFolderOptions struct {
	// ParentID optionally nests the new folder under an existing one.
	ParentID string
}

// CreateFolder creates a media folder and returns its ID.
func (m *Manager) CreateFolder(ctx context.Context, name string, opts CreateFolderOptions) (string, error) {
	folderID := shopware.UUID()
	entity := map[string]any{
		"id":            folderID,
		"name":          name,
		"parentId":      nullable(opts.ParentID),
		"configuration": map[string]any{},
	}
	repo := shopware.NewRepository[map[string]any](m.client, "media_folder")
	if err := repo.Upsert(ctx, []map[string]any{entity}); err != nil {
		return "", err
	}
	return folderID, nil
}

// splitFileName splits "logo.png" into ("logo", "png"). The extension is
// required and lower-cased; a dotted base name ("a.b.png") keeps its dots.
func splitFileName(fileName string) (baseName, extension string, err error) {
	base, ext, found := strings.CutLast(fileName, ".")
	if !found || base == "" || ext == "" {
		return "", "", fmt.Errorf("invalid file name %q: expected a name and an extension", fileName)
	}
	return base, strings.ToLower(ext), nil
}

// nullable returns nil for an empty string so the JSON field serializes as null
// rather than "".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
