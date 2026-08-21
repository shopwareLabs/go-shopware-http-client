package extension

import (
	"encoding/json/jsontext"
	"fmt"
)

// List is the response of ListAvailable.
type List []*Detail

// GetByName returns the extension with the given technical name, or nil.
func (l List) GetByName(name string) *Detail {
	for _, detail := range l {
		if detail.Name == name {
			return detail
		}
	}
	return nil
}

// FilterByUpdatable returns only the extensions that have a newer version
// available.
func (l List) FilterByUpdatable() List {
	out := make(List, 0)
	for _, detail := range l {
		if detail.IsUpdatable() {
			out = append(out, detail)
		}
	}
	return out
}

// Detail describes a single extension. Fields whose shape is not
// stable across Shopware versions are kept as jsontext.Value; decode them
// yourself when needed.
type Detail struct {
	Name            string  `json:"name"`
	Label           string  `json:"label"`
	Description     string  `json:"description"`
	ProducerName    string  `json:"producerName"`
	License         string  `json:"license"`
	Version         string  `json:"version"`
	LatestVersion   string  `json:"latestVersion"`
	NumberOfRatings int     `json:"numberOfRatings"`
	LocalID         string  `json:"localId"`
	Active          bool    `json:"active"`
	Type            string  `json:"type"`
	IsTheme         bool    `json:"isTheme"`
	Configurable    bool    `json:"configurable"`
	Source          string  `json:"source"`
	UpdateSource    string  `json:"updateSource"`
	IconRaw         *string `json:"iconRaw"`

	InstalledAt *Date `json:"installedAt"`
	UpdatedAt   *Date `json:"updatedAt"`

	// Permissions, Images, Categories, etc. vary by version/source.
	Permissions jsontext.Value `json:"permissions,omitempty"`
	Images      jsontext.Value `json:"images,omitempty"`
	Categories  jsontext.Value `json:"categories,omitempty"`
}

// Date is the PHP DateTime envelope Shopware serializes timestamps as.
type Date struct {
	Date         string `json:"date"`
	TimezoneType int    `json:"timezone_type"`
	Timezone     string `json:"timezone"`
}

// IsPlugin reports whether the extension is a plugin (vs an app).
func (e Detail) IsPlugin() bool {
	return e.Type == "plugin"
}

// IsUpdatable reports whether a newer version than the installed one exists.
func (e Detail) IsUpdatable() bool {
	return e.LatestVersion != "" && e.LatestVersion != e.Version
}

// Status returns a human-readable status line for the extension.
func (e Detail) Status() string {
	var text string
	switch {
	case e.Source == "store":
		text = "can be downloaded from store"
	case e.Active:
		text = "installed, activated"
	case e.InstalledAt != nil:
		text = "installed, not activated"
	default:
		text = "not installed, not activated"
	}

	if e.IsUpdatable() {
		text = fmt.Sprintf("%s, update available to %s", text, e.LatestVersion)
	}
	return text
}
