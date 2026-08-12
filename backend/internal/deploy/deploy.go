package deploy

import "fmt"

// Pipeline wires the build, versioning, and IIS-swap steps into one
// deploy operation.
type Pipeline struct {
	builder  *Builder
	versions *VersionStore
	iis      *IISSwapper
}

// NewPipeline returns a Pipeline using the given collaborators.
func NewPipeline(builder *Builder, versions *VersionStore, iis *IISSwapper) *Pipeline {
	return &Pipeline{builder: builder, versions: versions, iis: iis}
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
func (p *Pipeline) Deploy(sourceDir string, recipe Recipe, repo, environment, siteName string, keepVersions int) (string, error) {
	releaseDir, err := p.versions.NewRelease(repo, environment)
	if err != nil {
		return "", fmt.Errorf("deploy: failed to allocate release dir: %w", err)
	}

	if err := p.builder.Build(sourceDir, recipe, releaseDir); err != nil {
		return "", fmt.Errorf("deploy: build failed: %w", err)
	}

	if err := p.iis.SetPhysicalPath(siteName, releaseDir); err != nil {
		return "", fmt.Errorf("deploy: failed to activate release: %w", err)
	}

	if err := p.versions.Prune(repo, environment, keepVersions); err != nil {
		return "", fmt.Errorf("deploy: deploy succeeded but pruning old releases failed: %w", err)
	}

	return releaseDir, nil
}
