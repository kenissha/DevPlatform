package iishelper

import (
	"encoding/json"
	"fmt"
	"os"
)

// targetEntry mirrors only the one field of internal/deployment.Target
// this package cares about. Deliberately not importing internal/deployment
// itself — this package's job (deciding which physical-path changes are
// allowed) is independent of that package's request/approval business
// logic, and duplicating one field name is cheaper than adding a
// dependency between them.
type targetEntry struct {
	SiteName string `json:"siteName"`
}

// LoadAllowedSites reads the same deploy targets file
// internal/deployment.LoadTargets reads (path comes from the same
// DEVPLATFORM_DEPLOY_TARGETS_FILE environment variable) and returns the
// set of SiteName values it declares — the only sites this helper will
// ever agree to repoint. An empty path returns an empty set with no
// error, matching this codebase's established "no targets file
// configured means nothing is deployable" safe default.
func LoadAllowedSites(path string) (map[string]bool, error) {
	sites := map[string]bool{}
	if path == "" {
		return sites, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("iishelper: failed to read deploy targets file %q: %w", path, err)
	}

	var entries []targetEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("iishelper: failed to parse deploy targets file %q: %w", path, err)
	}

	for _, e := range entries {
		if e.SiteName != "" {
			sites[e.SiteName] = true
		}
	}
	return sites, nil
}
