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
// DEVPLATFORM_DEPLOY_TARGETS_FILE is read directly via os.Getenv rather
// than through internal/config.Load(), which would pull in config fields
// (SMTP, JWT secret, etc.) this single-purpose binary has no use for.
//
// DEVPLATFORM_IISHELPER_SDDL is an optional Windows security descriptor
// string restricting which account may connect to the named pipe. Left
// empty, go-winio applies its own default pipe security (owner and
// Administrators only) — safe for local development, but production
// should set this explicitly to the one account devplatform.exe runs
// as (see the install script for how to generate this value).
func setup() (net.Listener, *iishelper.Server, error) {
	targetsFile := os.Getenv("DEVPLATFORM_DEPLOY_TARGETS_FILE")
	allowedSites, err := iishelper.LoadAllowedSites(targetsFile)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("iishelper: %d allowed site(s) loaded from %q", len(allowedSites), targetsFile)

	var pipeConfig *winio.PipeConfig
	if sddl := os.Getenv("DEVPLATFORM_IISHELPER_SDDL"); sddl != "" {
		pipeConfig = &winio.PipeConfig{SecurityDescriptor: sddl}
	}
	ln, err := winio.ListenPipe(iishelper.PipeName, pipeConfig)
	if err != nil {
		return nil, nil, err
	}

	srv := &iishelper.Server{
		AppcmdPath:   deploy.AppcmdPath(),
		AllowedSites: allowedSites,
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
