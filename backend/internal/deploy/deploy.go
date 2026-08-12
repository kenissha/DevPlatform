package deploy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kenissha/DevPlatform/backend/internal/secretsvault"
)

// ErrPruneFailed indicates that a release was successfully built and
// activated (the site IS live on the new release) but pruning old releases
// afterward failed. Callers can distinguish this from a genuine deploy
// failure via errors.Is(err, ErrPruneFailed); Deploy still returns the
// valid, now-live release directory in this case, not an empty string.
var ErrPruneFailed = errors.New("deploy: release activated but pruning old releases failed")

// releaseStore is the subset of VersionStore's behavior Pipeline depends
// on, abstracted so tests can simulate a Prune failure without relying on
// filesystem-level tricks (permissions, locked files) that are unreliable
// across platforms. *VersionStore already satisfies this interface with
// no changes needed.
type releaseStore interface {
	NewRelease(repo, environment string) (string, error)
	Prune(repo, environment string, keep int) error
}

// Pipeline wires the build, versioning, and IIS-swap steps into one
// deploy operation.
type Pipeline struct {
	builder  *Builder
	versions releaseStore
	iis      *IISSwapper
	secrets  *secretsvault.Store
}

// NewPipeline returns a Pipeline using the given collaborators. secrets may
// be nil if this Pipeline will never be asked to inject secrets (Deploy
// will then error if a caller passes a non-empty secretsTarget).
func NewPipeline(builder *Builder, versions releaseStore, iis *IISSwapper, secrets *secretsvault.Store) *Pipeline {
	return &Pipeline{builder: builder, versions: versions, iis: iis, secrets: secrets}
}

// Deploy builds sourceDir with recipe, writes the result to a fresh
// versioned release directory for (repo, environment), points siteName at
// that directory, and prunes old releases beyond keepVersions. Returns the
// new release directory's path.
//
// If the build step fails, no release directory is left half-written that
// could later be mistaken for a real release: NewRelease only creates the
// directory, and a failed Build leaves an empty (not partially-populated
// in a way that matters — build tools are expected to fail before writing
// partial output, which both dotnet publish and this plan's npm fixture
// do) directory behind. Cleaning up an empty failed-release directory is
// not implemented in this proof-of-concept task; note it as a follow-up
// if this pipeline is extended to handle build failures more gracefully.
func (p *Pipeline) Deploy(sourceDir string, recipe Recipe, repo, environment, siteName string, keepVersions int, secretsTarget string) (string, error) {
	// keepVersions < 1 would prune the release this call just activated:
	// Prune keeps only the newest `keep` releases, and the release created
	// below is always the newest at the point Prune runs. Reject before
	// doing any work — there's no point allocating a release directory or
	// building anything for a call that's going to be rejected anyway.
	if keepVersions < 1 {
		return "", fmt.Errorf("deploy: keepVersions must be at least 1, got %d", keepVersions)
	}

	releaseDir, err := p.versions.NewRelease(repo, environment)
	if err != nil {
		return "", fmt.Errorf("deploy: failed to allocate release dir: %w", err)
	}

	if err := p.builder.Build(sourceDir, recipe, releaseDir); err != nil {
		return "", fmt.Errorf("deploy: build failed: %w", err)
	}

	if secretsTarget != "" && !filepath.IsLocal(secretsTarget) {
		return "", fmt.Errorf("deploy: invalid secretsTarget %q", secretsTarget)
	}

	if secretsTarget != "" {
		if p.secrets == nil {
			return "", fmt.Errorf("deploy: secretsTarget %q given but no secrets store is configured", secretsTarget)
		}
		plaintext, err := p.secrets.Get(repo, environment)
		if err != nil {
			return "", fmt.Errorf("deploy: failed to load secrets: %w", err)
		}
		if err := os.WriteFile(filepath.Join(releaseDir, secretsTarget), plaintext, 0o640); err != nil {
			return "", fmt.Errorf("deploy: failed to write secrets into release: %w", err)
		}
	}

	if err := p.iis.SetPhysicalPath(siteName, releaseDir); err != nil {
		return "", fmt.Errorf("deploy: failed to activate release: %w", err)
	}

	if err := p.versions.Prune(repo, environment, keepVersions); err != nil {
		// The release is already live (SetPhysicalPath succeeded above) —
		// return the valid releaseDir alongside the wrapped sentinel so
		// callers can tell this apart from an actual deploy failure via
		// errors.Is(err, ErrPruneFailed) instead of losing the release
		// path a real deploy did produce.
		return releaseDir, fmt.Errorf("%w: %v", ErrPruneFailed, err)
	}

	return releaseDir, nil
}
