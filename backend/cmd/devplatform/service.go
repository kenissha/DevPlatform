package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"golang.org/x/sys/windows/svc"
)

// serviceName must match the name install.ps1 registers with the Service
// Control Manager.
const serviceName = "DevPlatform"

// shutdownGrace bounds how long a stopping service waits for in-flight
// requests to finish before forcing the listener closed. A deploy can run
// for minutes (see internal/deployment's own deployTimeout), so a stop
// issued mid-deploy will still cut it short — but the SCM kills a service
// that takes too long to stop anyway, and leaving a half-finished deploy
// is recoverable (the request is recorded as failed and can be reopened),
// whereas an unstoppable service is not.
const shutdownGrace = 30 * time.Second

// windowsService adapts the HTTP server to the Windows Service Control
// Manager's start/stop/shutdown protocol.
//
// Running as a service rather than under IIS's httpPlatformHandler is
// deliberate: this process does work that is not driven by incoming HTTP
// requests — the nightly repository backup runs on a timer, and an
// approved deploy runs for minutes after the request that triggered it
// has already been answered. A request-lifecycle host is free to idle out
// or recycle the process between requests, which would silently skip a
// backup or truncate a deploy. A service stays up because Windows keeps
// it up, independent of traffic.
type windowsService struct {
	server *http.Server
}

func (w *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	done := make(chan error, 1)
	go func() { done <- w.server.ListenAndServe() }()

	s <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			// ErrServerClosed here means something called Shutdown/Close
			// without going through the SCM stop path below — not an error,
			// but not expected either.
			if errors.Is(err, http.ErrServerClosed) {
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			}
			log.Printf("devplatform: server stopped unexpectedly: %v", err)
			return false, 1
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				s <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}

				ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
				if err := w.server.Shutdown(ctx); err != nil {
					log.Printf("devplatform: graceful shutdown did not complete (%v); closing anyway", err)
					w.server.Close()
				}
				cancel()

				<-done
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}
