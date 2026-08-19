// Command deploydemo is a throwaway manual-verification tool for the
// internal/deploy package's Pipeline — it is not part of the DevPlatform
// server and is not wired into main.go. It deploys the npm test fixture
// to a real IIS site via the real appcmd.exe, so a human can watch the
// whole build -> version -> swap -> rollback cycle happen for real, once,
// before this mechanism is trusted with anything that matters.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

func main() {
	siteName := flag.String("site", "DevPlatform Test Site", "IIS site name to deploy to")
	dataDir := flag.String("data-dir", "./deploydemo-data", "where release folders are stored")
	flag.Parse()

	sourceDir, err := filepath.Abs("internal/deploy/testdata/npm-fixture")
	if err != nil {
		log.Fatal(err)
	}

	// IIS requires an absolute physical path — the worker process's working
	// directory has nothing to do with wherever this CLI happened to be
	// run from, so a relative -data-dir (including the flag's own default)
	// must be resolved to an absolute path before VersionStore ever builds
	// a release path from it.
	absDataDir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}

	vs := deploy.NewVersionStore(absDataDir)
	pipeline := deploy.NewPipeline(&deploy.Builder{}, vs, deploy.NewIISSwapper(deploy.RealCommandRunner{}), nil)

	releaseDir, err := pipeline.Deploy(sourceDir, deploy.RecipeNpm, "demo", "test", *siteName, "", 5, "")
	if err != nil {
		log.Fatalf("deploy failed: %v", err)
	}

	fmt.Printf("Deployed to %s\nRelease directory: %s\n", *siteName, releaseDir)
	_ = os.Stdout
}
