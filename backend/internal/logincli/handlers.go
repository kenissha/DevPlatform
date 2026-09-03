// Package logincli serves the devplatform-login CLI tool (see
// backend/cmd/devplatform-login) and a one-line PowerShell installer
// for it, so a developer sets it up with
// `irm https://<host>/api/devplatform-login/install.ps1 | iex` instead of
// the exe being manually copied from machine to machine. Unauthenticated
// by design: the binary itself carries no secrets, and downloading it
// grants no access — real Intranet AD credentials are still required
// the first time it actually runs.
package logincli

import (
	"fmt"
	"net/http"
	"os"
)

// Handlers serves the binary at Path (built with
// `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`, see
// docs/DURUM.md's 2026-09-03 entry for why those flags matter on at
// least one real deployment machine) and a matching install script.
type Handlers struct {
	// Path is the absolute path to a built devplatform-login.exe on
	// disk. Empty means not configured — both handlers respond 404,
	// the same "nothing until deliberately configured" pattern as
	// internal/config's AllowedSitesFile/BackupDir.
	Path string
	// BaseURL is this deployment's real externally-visible origin
	// (e.g. "https://git.sigortatahkim.org"), the same value
	// internal/config.Config.BaseURL already provides for notification
	// links. InstallScript needs this rather than the incoming
	// request's Host header — in production this process sits behind
	// IIS's reverse proxy and only ever sees the internal loopback
	// address (e.g. 127.0.0.1:8082) as its Host, discovered live
	// (2026-09-03) when the generated script tried to download from
	// that internal address and failed with "cannot connect to the
	// remote server". Falls back to the request's own Host when empty,
	// which is fine for local, non-proxied development.
	BaseURL string
}

// Download serves the raw exe.
func (h *Handlers) Download(w http.ResponseWriter, r *http.Request) {
	if h.Path == "" {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}
	if _, err := os.Stat(h.Path); err != nil {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="devplatform-login.exe"`)
	http.ServeFile(w, r, h.Path)
}

// InstallScript serves a small PowerShell script that downloads the exe
// to a fixed local path and runs `install` — the "irm ... | iex" entry
// point.
func (h *Handlers) InstallScript(w http.ResponseWriter, r *http.Request) {
	if h.Path == "" {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}
	base := h.BaseURL
	if base == "" {
		base = "https://" + r.Host
	}
	downloadURL := base + "/api/devplatform-login.exe"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, installScriptTemplate, downloadURL)
}

const installScriptTemplate = `$ErrorActionPreference = 'Stop'
$dest = Join-Path $env:LOCALAPPDATA 'devplatform\devplatform-login.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $dest) | Out-Null
Write-Host "devplatform-login indiriliyor..."
Invoke-WebRequest -Uri '%s' -OutFile $dest
& $dest install
Write-Host "Kuruldu: $dest"
`
