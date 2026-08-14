package main

import (
	"net/http"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

// waitForState drains status updates until the expected state arrives, so
// a test doesn't depend on how many intermediate states Execute reports.
func waitForState(t *testing.T, status <-chan svc.Status, want svc.State) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case got := <-status:
			if got.State == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for service state %v", want)
		}
	}
}

func TestWindowsService_StopsCleanlyOnAnSCMStopRequest(t *testing.T) {
	// Port 0 lets the OS pick a free port — this test only cares that the
	// server starts and stops, not which address it lands on.
	ws := &windowsService{server: &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}}

	requests := make(chan svc.ChangeRequest, 1)
	status := make(chan svc.Status, 16)

	type result struct {
		svcSpecific bool
		exitCode    uint32
	}
	returned := make(chan result, 1)
	go func() {
		svcSpecific, exitCode := ws.Execute(nil, requests, status)
		returned <- result{svcSpecific, exitCode}
	}()

	waitForState(t, status, svc.Running)

	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	select {
	case got := <-returned:
		if got.exitCode != 0 {
			t.Errorf("exit code = %d, want 0 for a clean stop", got.exitCode)
		}
		if got.svcSpecific {
			t.Error("svcSpecificEC = true, want false")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after a Stop request")
	}
}

func TestWindowsService_ReportsRunningBeforeAcceptingStop(t *testing.T) {
	ws := &windowsService{server: &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}}

	requests := make(chan svc.ChangeRequest, 1)
	status := make(chan svc.Status, 16)

	go ws.Execute(nil, requests, status)

	// The SCM kills a service that never reports Running, so this
	// transition is load-bearing, not cosmetic.
	first := <-status
	if first.State != svc.StartPending {
		t.Errorf("first reported state = %v, want StartPending", first.State)
	}

	second := <-status
	if second.State != svc.Running {
		t.Fatalf("second reported state = %v, want Running", second.State)
	}
	if second.Accepts&svc.AcceptStop == 0 {
		t.Error("Running status does not accept Stop, so the service could never be stopped")
	}
	if second.Accepts&svc.AcceptShutdown == 0 {
		t.Error("Running status does not accept Shutdown, so a machine restart would kill it abruptly")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Stop}
}
