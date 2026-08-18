package iishelper

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadAllowedSites reads a JSON array of IIS site names from path — the
// only sites this helper will ever agree to repoint. This file is
// deliberately separate from internal/deployment's panel-writable
// target store: it is the actual security boundary (see
// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md's
// "Güvenlik" section), so it is edited only by hand on the server,
// pointed at via DEVPLATFORM_ALLOWED_SITES_FILE, and read once at this
// process's startup — never through any API. An empty path returns an
// empty set with no error, matching this codebase's established "no
// file configured means nothing is allowed" safe default.
func LoadAllowedSites(path string) (map[string]bool, error) {
	sites := map[string]bool{}
	if path == "" {
		return sites, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("iishelper: failed to read allowed sites file %q: %w", path, err)
	}

	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("iishelper: failed to parse allowed sites file %q: %w", path, err)
	}

	for _, name := range names {
		if name != "" {
			sites[name] = true
		}
	}
	return sites, nil
}
