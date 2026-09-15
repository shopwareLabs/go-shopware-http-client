# Media

[← Docs index](./README.md)

The `media` sub-package handles uploading media (by binary content or by URL)
and resolving/creating media folders. It is built from the same
`EntityRepository` + `Criteria` primitives as the rest of the package.

```go
import (
	shopware "github.com/shopwareLabs/go-shopware-http-client"
	"github.com/shopwareLabs/go-shopware-http-client/media"
)

mgr := media.NewManager(client)
```

## Uploading a file

Pass any `io.Reader` for the content. The manager creates the media entity,
uploads the binary, and — if the upload fails — rolls the entity back
automatically.

```go
f, _ := os.Open("logo.png")
defer f.Close()

id, err := mgr.Upload(ctx, f, media.UploadOptions{
	FileName:    "logo.png",   // extension required
	ContentType: "image/png",  // optional; inferred from the extension if empty
	Private:     false,
	MediaFolderID: folderID,    // optional
})
```

## Uploading by URL

The server downloads the content from the given URL.

```go
id, err := mgr.UploadByURL(ctx, media.UploadURLOptions{
	FileName: "image.jpg",
	URL:      "https://example.com/image.jpg",
})
```

## Media folders

```go
// Default folder configured for an entity (e.g. "product"), or "" if none.
folderID, err := mgr.DefaultFolderByEntity(ctx, "product")

// Look up a folder by name, or "" if not found.
folderID, err = mgr.FolderByName(ctx, "Product Media")

// Create a folder (optionally nested), returns the new id.
id, err := mgr.CreateFolder(ctx, "Imports", media.CreateFolderOptions{
	ParentID: parentID, // optional
})
```

## See also

- [Entities & repositories](./entities.md) — the repository layer these helpers use.
- [Raw requests](./raw-requests.md) — `RequestRaw`, which the binary upload builds on.
