// Command iishelper is the one process in this platform allowed to run
// appcmd.exe. It runs as a Windows Service (LocalSystem), listens on a
// local named pipe, and only ever executes the one operation
// iishelper.ValidateRequest accepts — see internal/iishelper's package
// doc comment for the full picture.
package main

import (
	"log"
	"net"
	"os"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows/svc"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
	"github.com/kenissha/DevPlatform/backend/internal/iishelper"
)

const serviceName = "DevPlatformIISHelper"

func main() {
	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("iishelper: failed to determine execution context: %v", err)
	}

	ln, srv, err := setup()
	if err != nil {
		log.Fatalf("iishelper: setup failed: %v", err)
	}

	if isService {
		if err := svc.Run(serviceName, &windowsService{listener: ln, server: srv}); err != nil {
			log.Fatalf("iishelper: service run failed: %v", err)
		}
		return
	}

	// Not running under the Service Control Manager — e.g. a developer
	// running iishelper.exe directly from a console during testing. Serve
	// until the process is killed.
	log.Printf("iishelper: running interactively on %s (not installed as a Windows Service)", iishelper.PipeName)
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("iishelper: %v", err)
	}
}

// setup loads configuration, opens the named pipe listener, and builds
// the Server that will handle requests on it. Split out from main so
// both the Windows-Service path and the interactive (development) path
// share identical setup, and so it can be tested without needing a real
// Service Control Manager.
//
// DEVPLATFORM_ALLOWED_SITES_FILE is read directly via os.Getenv rather
// than through internal/config.Load(), which would pull in config fields
// (SMTP, JWT secret, etc.) this single-purpose binary has no use for. It
// points at a small, ops-edited JSON array of IIS site names — see
// internal/iishelper.LoadAllowedSites's doc comment for why this file is
// deliberately not the same one internal/deployment's panel-writable
// Store uses.
//
// DEVPLATFORM_IISHELPER_SDDL is an optional Windows security descriptor
// string restricting which account may connect to the named pipe. Left
// empty, go-winio applies its own default pipe security, which also
// grants Everyone read access — fine for local development, but not
// something production should rely on for restricting access. Production
// should set this explicitly to the one account devplatform.exe runs
// as (see the install script for how to generate this value).
//
// DEVPLATFORM_RELEASES_ROOT must be set to the exact same absolute path
// devplatform.exe passes to deploy.NewVersionStore (i.e. its DataDir's
// "releases" subdirectory) — it is the one directory tree a physical
// path is ever allowed to point into (see iishelper.ValidateRequest's
// doc comment). Left empty, every request is rejected: this check fails
// closed, not open.
func setup() (net.Listener, *iishelper.Server, error) {
	sitesFile := os.Getenv("DEVPLATFORM_ALLOWED_SITES_FILE")
	allowedSites, err := iishelper.LoadAllowedSites(sitesFile)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("iishelper: %d allowed site(s) loaded from %q", len(allowedSites), sitesFile)

	releasesRoot := os.Getenv("DEVPLATFORM_RELEASES_ROOT")
	if releasesRoot == "" {
		log.Printf("iishelper: WARNING - DEVPLATFORM_RELEASES_ROOT is not set; every deploy request will be rejected until it is")
	}

	var pipeConfig *winio.PipeConfig
	if sddl := os.Getenv("DEVPLATFORM_IISHELPER_SDDL"); sddl != "" {
		pipeConfig = &winio.PipeConfig{SecurityDescriptor: sddl}
	} else {
		log.Printf("iishelper: WARNING - DEVPLATFORM_IISHELPER_SDDL is not set; the named pipe is using go-winio's default security (readable by Everyone, not restricted to devplatform.exe's account) - see install.ps1 for how to set this before production use")
	}
	ln, err := winio.ListenPipe(iishelper.PipeName, pipeConfig)
	if err != nil {
		return nil, nil, err
	}

	srv := &iishelper.Server{
		AppcmdPath:   deploy.AppcmdPath(),
		AllowedSites: allowedSites,
		ReleasesRoot: releasesRoot,
		Execute:      deploy.RealCommandRunner{}.Run,
	}
	return ln, srv, nil
}

// windowsService adapts Server.Serve to the Windows Service Control
// Manager's start/stop/shutdown protocol.
type windowsService struct {
	listener net.Listener
	server   *iishelper.Server
}

func (w *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	done := make(chan error, 1)
	go func() { done <- w.server.Serve(w.listener) }()

	s <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			log.Printf("iishelper: server stopped unexpectedly: %v", err)
			return false, 1
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				s <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				w.listener.Close()
				<-done
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}
