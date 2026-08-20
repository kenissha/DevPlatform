# Process Tabanlı Backend Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `dotnet` tarifiyle build edilen deploy hedefleri için, bir process'in kendi dosyalarını kilitlemesi yüzünden çalışmayan bare `physicalPath` swap yerine, durdur→değiştir→başlat akışını (başlatma başarısız olursa otomatik önceki versiyona dönüşle birlikte) hem Approve hem Rollback'e eklemek.

**Architecture:** `IISSwapper`'a iki yeni appcmd işlemi (`StopSite`/`StartSite`) ve bunları Recipe'e göre orkestre eden tek bir `ActivateRelease` metodu eklenir. Hem `Pipeline.Deploy` (yeni deploy) hem `deployment.Handlers.Rollback` (geri dönme) artık `SetPhysicalPath`'i doğrudan değil, `ActivateRelease` üzerinden çağırır — davranış farkı tamamen bu tek metodun içinde, iki çağıran yer değişmez kalır.

**Tech Stack:** Go (backend, mevcut kod tabanı), appcmd.exe (IIS), mevcut `iishelper` named-pipe protokolü.

**Spec:** `docs/superpowers/specs/2026-08-19-process-based-backend-deploy-design.md`

## Global Constraints

- `iishelper`'ın izin verdiği appcmd şekilleri sabit ve kapalı bir liste olmaya devam eder — yeni şekiller de (eskisi gibi) sadece `allowed-sites.json`'daki site adlarına izin verir, yeni bir izin listesi/eşleme eklenmez.
- `npm` tarifi hiç değişmez — sadece `dotnet` tarifi yeni davranışı alır.
- Başarısız bir deploy/rollback denemesi asla eski versiyonları (`Prune`) silmez — sadece başarıyla aktive edilen bir release'in ardından çalışır (mevcut davranış, korunuyor).
- Her adım gerçek appcmd'ye dokunmadan, `fakeCommandRunner` ile test edilir (mevcut desen).

---

### Task 1: iishelper — `stop site`/`start site` şekillerini kabul et

**Files:**
- Modify: `backend/internal/iishelper/validate.go`
- Test: `backend/internal/iishelper/validate_test.go`

**Interfaces:**
- Consumes: yok (bu paket kendi başına, dışarıya bağımlı değil).
- Produces: `ValidateRequest` artık üç şekli kabul ediyor — `set vdir .../physicalPath:...` (değişmedi), `stop site /site.name:<site>`, `start site /site.name:<site>`. İmza değişmedi: `ValidateRequest(req Request, appcmdPath string, allowedSites map[string]bool, releasesRoot string) error`.

- [ ] **Step 1: Mevcut `ValidateRequest`'i oku, dispatch'e hazırlan**

`backend/internal/iishelper/validate.go`'nun tamamını oku (önceki oturumda değiştirilmişti, `set vdir` + `releasesRoot` kontrolünü içeriyor).

- [ ] **Step 2: Yeni testleri yaz (önce test, sonra kod)**

`validate_test.go`'nun sonuna ekle:

```go
func TestValidateRequest_AcceptsStopSiteForAnAllowedSite(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"stop", "site", `/site.name:DevPlatform Test Site`},
	}
	if err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot); err != nil {
		t.Fatalf("expected stop site to be accepted for an allowed site, got: %v", err)
	}
}

func TestValidateRequest_AcceptsStartSiteForAnAllowedSite(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"start", "site", `/site.name:DevPlatform Test Site`},
	}
	if err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot); err != nil {
		t.Fatalf("expected start site to be accepted for an allowed site, got: %v", err)
	}
}

func TestValidateRequest_RejectsStopSiteForAnUnlistedSite(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"stop", "site", `/site.name:Some Other Site`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an unlisted site, got: %v", err)
	}
}

func TestValidateRequest_RejectsStartSiteForAnUnlistedSite(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"start", "site", `/site.name:Some Other Site`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an unlisted site, got: %v", err)
	}
}

func TestValidateRequest_RejectsSiteLifecycleMissingSiteNamePrefix(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"stop", "site", `DevPlatform Test Site`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a third argument missing /site.name:, got: %v", err)
	}
}

func TestValidateRequest_RejectsUnrecognizedVerb(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"delete", "site", `/site.name:DevPlatform Test Site`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an unrecognized verb, got: %v", err)
	}
}

func TestValidateRequest_RejectsStopSiteWithWrongArgumentCount(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"stop", "site"},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites(), testReleasesRoot)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a short argument list, got: %v", err)
	}
}
```

- [ ] **Step 2b: Testleri çalıştır, başarısız olduklarını doğrula**

Run: `go test ./internal/iishelper/... -run TestValidateRequest -v`
Expected: yukarıdaki 6 yeni test FAIL (henüz `stop`/`start` desteklenmiyor, hepsi "unexpected verb" ya da "expected exactly 4 arguments" tarzı hatalarla `ErrInvalidRequest` dönüyor olabilir bile — ama `TestValidateRequest_AcceptsStopSiteForAnAllowedSite` ve `AcceptsStartSiteForAnAllowedSite` kesin FAIL etmeli, çünkü şu an hata bekliyor değil, hatasız geçmesini bekliyorlar).

- [ ] **Step 3: `ValidateRequest`'i dispatch yapısına çevir**

`validate.go`'da `ValidateRequest` fonksiyonunun tamamını şu şekilde değiştir (mevcut `set vdir` mantığı `validatePhysicalPathSwap`'e taşınıyor, birebir aynı kalıyor — sadece isim/konum değişiyor):

```go
// ValidateRequest is the actual security boundary of this package: it
// never trusts req as coming from a well-behaved devplatform.exe. It
// independently re-derives what a legitimate request must look like and
// rejects anything that deviates from one of the small, fixed set of
// operations this helper is willing to perform:
//
//	appcmd.exe set vdir "<one of allowedSites>/" /physicalPath:<path under releasesRoot>
//	appcmd.exe stop site /site.name:"<one of allowedSites>"
//	appcmd.exe start site /site.name:"<one of allowedSites>"
//
// The latter two exist for process-based (dotnet-recipe) deploy targets:
// a running process locks its own files, so a bare physical-path swap
// doesn't make it pick up a new release the way it does for a static
// site — see docs/superpowers/specs/2026-08-19-process-based-backend-deploy-design.md.
// Both are gated by the exact same allowedSites set as the physical-path
// swap: iishelper never learns or cares which sites are process-based,
// that decision lives entirely in the deployment package.
func ValidateRequest(req Request, appcmdPath string, allowedSites map[string]bool, releasesRoot string) error {
	if req.Name != appcmdPath {
		return fmt.Errorf("%w: unexpected program %q", ErrInvalidRequest, req.Name)
	}

	switch {
	case len(req.Args) == 4 && req.Args[0] == "set" && req.Args[1] == "vdir":
		return validatePhysicalPathSwap(req.Args, allowedSites, releasesRoot)
	case len(req.Args) == 3 && (req.Args[0] == "stop" || req.Args[0] == "start") && req.Args[1] == "site":
		return validateSiteLifecycle(req.Args, allowedSites)
	default:
		return fmt.Errorf("%w: unrecognized command shape", ErrInvalidRequest)
	}
}

// validatePhysicalPathSwap validates the "set vdir .../physicalPath:..."
// shape — unchanged from before this function was split out of
// ValidateRequest.
func validatePhysicalPathSwap(args []string, allowedSites map[string]bool, releasesRoot string) error {
	site, ok := strings.CutSuffix(args[2], "/")
	if !ok {
		return fmt.Errorf("%w: site argument %q must end with /", ErrInvalidRequest, args[2])
	}
	if !allowedSites[site] {
		return fmt.Errorf("%w: %q is not a configured deploy target site", ErrInvalidRequest, site)
	}

	path, ok := strings.CutPrefix(args[3], "/physicalPath:")
	if !ok {
		return fmt.Errorf("%w: fourth argument must start with /physicalPath:", ErrInvalidRequest)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: physical path %q must be absolute", ErrInvalidRequest, path)
	}
	if releasesRoot == "" {
		return fmt.Errorf("%w: no releases root is configured, refusing every physical path", ErrInvalidRequest)
	}
	rel, err := filepath.Rel(releasesRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: physical path %q is outside the configured releases root %q", ErrInvalidRequest, path, releasesRoot)
	}
	return nil
}

// validateSiteLifecycle validates "stop site /site.name:<site>" and
// "start site /site.name:<site>" — args[0] is already known to be "stop"
// or "start" and args[1] already known to be "site" by the caller's
// switch, so only the site-name argument needs checking here.
func validateSiteLifecycle(args []string, allowedSites map[string]bool) error {
	site, ok := strings.CutPrefix(args[2], "/site.name:")
	if !ok {
		return fmt.Errorf("%w: third argument must start with /site.name:", ErrInvalidRequest)
	}
	if !allowedSites[site] {
		return fmt.Errorf("%w: %q is not a configured deploy target site", ErrInvalidRequest, site)
	}
	return nil
}
```

- [ ] **Step 4: Testleri tekrar çalıştır**

Run: `go test ./internal/iishelper/... -v`
Expected: PASS — hem yeni 6 test hem paketteki tüm eski testler (özellikle `TestValidateRequest_AcceptsTheOneAllowedShape` ve diğer `set vdir` testleri, davranışları değişmediği için).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/iishelper/validate.go backend/internal/iishelper/validate_test.go
git commit -m "feat(iishelper): accept stop/start site alongside set vdir"
```

---

### Task 2: `IISSwapper`'a `StopSite`/`StartSite` ekle

**Files:**
- Modify: `backend/internal/deploy/iisswap.go`
- Test: `backend/internal/deploy/iisswap_test.go`

**Interfaces:**
- Consumes: mevcut `CommandRunner` arayüzü (`Run(name string, args ...string) ([]byte, error)`), mevcut `AppcmdPath()`.
- Produces: `(*IISSwapper) StopSite(siteName string) error` ve `(*IISSwapper) StartSite(siteName string) error`.

- [ ] **Step 1: Testleri yaz**

`iisswap_test.go`'nun sonuna ekle:

```go
func TestStopSite_InvokesAppcmdWithExpectedArguments(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	if err := swapper.StopSite("DevPlatform Test Site"); err != nil {
		t.Fatalf("StopSite returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(runner.calls))
	}
	want := []string{AppcmdPath(), "stop", "site", "/site.name:DevPlatform Test Site"}
	got := runner.calls[0]
	if len(got) != len(want) {
		t.Fatalf("call = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStartSite_InvokesAppcmdWithExpectedArguments(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	if err := swapper.StartSite("DevPlatform Test Site"); err != nil {
		t.Fatalf("StartSite returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(runner.calls))
	}
	want := []string{AppcmdPath(), "start", "site", "/site.name:DevPlatform Test Site"}
	got := runner.calls[0]
	if len(got) != len(want) {
		t.Fatalf("call = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStopSite_PropagatesCommandRunnerError(t *testing.T) {
	runner := &fakeCommandRunner{failWith: errors.New("appcmd exited 5: access denied")}
	swapper := NewIISSwapper(runner)

	if err := swapper.StopSite("DevPlatform Test Site"); err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestStartSite_PropagatesCommandRunnerError(t *testing.T) {
	runner := &fakeCommandRunner{failWith: errors.New("appcmd exited 5: access denied")}
	swapper := NewIISSwapper(runner)

	if err := swapper.StartSite("DevPlatform Test Site"); err == nil {
		t.Fatal("expected an error, got nil")
	}
}
```

- [ ] **Step 2: Testleri çalıştır, başarısız olduklarını doğrula**

Run: `go test ./internal/deploy/... -run "TestStopSite|TestStartSite" -v`
Expected: FAIL — derleme hatası, `StopSite`/`StartSite` metodları yok.

- [ ] **Step 3: Metodları ekle**

`iisswap.go`'da `SetPhysicalPath`'in hemen altına ekle:

```go
// StopSite stops siteName via appcmd.exe — the first half of the
// stop→swap→start sequence a process-based (dotnet-recipe) release needs,
// since a running process locks its own files and a bare physical-path
// swap alone never makes it pick up a new release. siteName must already
// be validated/trusted by the caller, same as SetPhysicalPath.
func (s *IISSwapper) StopSite(siteName string) error {
	_, err := s.runner.Run(AppcmdPath(), "stop", "site", "/site.name:"+siteName)
	if err != nil {
		return fmt.Errorf("deploy: failed to stop site %q: %w", siteName, err)
	}
	return nil
}

// StartSite starts siteName via appcmd.exe — see StopSite.
func (s *IISSwapper) StartSite(siteName string) error {
	_, err := s.runner.Run(AppcmdPath(), "start", "site", "/site.name:"+siteName)
	if err != nil {
		return fmt.Errorf("deploy: failed to start site %q: %w", siteName, err)
	}
	return nil
}
```

- [ ] **Step 4: Testleri tekrar çalıştır**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, tüm paket.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/deploy/iisswap.go backend/internal/deploy/iisswap_test.go
git commit -m "feat(deploy): add IISSwapper.StopSite/StartSite"
```

---

### Task 3: `ActivateRelease` — Recipe'e göre orkestrasyon + otomatik geri dönüş

**Files:**
- Modify: `backend/internal/deploy/iisswap.go`
- Test: `backend/internal/deploy/iisswap_test.go`

**Interfaces:**
- Consumes: Task 2'nin `StopSite`/`StartSite`'ı, mevcut `SetPhysicalPath`, `Recipe`/`RecipeDotnet`/`RecipeNpm` (zaten `build.go`'da tanımlı, aynı paket).
- Produces: `(*IISSwapper) ActivateRelease(recipe Recipe, siteName, newReleaseDir, previousReleaseDir string) error`, iki yeni sentinel hata: `ErrReverted`, `ErrSiteDown`. Task 4 bunları kullanacak.

- [ ] **Step 1: Test fake'ini genişlet — `start` çağrılarını sırayla başarısız/başarılı yapabilme**

`iisswap_test.go`'nun başındaki `fakeCommandRunner` tanımını şu şekilde değiştir (mevcut `failWith` alanı kalıyor, yeni bir alan ekleniyor):

```go
// fakeCommandRunner records every call it receives instead of executing
// anything real — this is how IISSwapper's argument-building logic gets
// tested without ever invoking the real appcmd.exe, which requires
// Administrator privileges this test environment doesn't have.
type fakeCommandRunner struct {
	calls    [][]string
	failWith error
	// failStart is consumed in order, one entry per "start site" call —
	// ActivateRelease's auto-revert path makes two such calls (the new
	// release, then the previous one on failure) and tests need to make
	// the first fail while the second succeeds (or both fail), which a
	// single blanket failWith can't express since neither call carries
	// any argument that tells them apart (StartSite takes no path).
	failStart []error
}

func (f *fakeCommandRunner) Run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if f.failWith != nil {
		return nil, f.failWith
	}
	if len(args) > 0 && args[0] == "start" && len(f.failStart) > 0 {
		err := f.failStart[0]
		f.failStart = f.failStart[1:]
		if err != nil {
			return nil, err
		}
	}
	return []byte("ok"), nil
}
```

- [ ] **Step 2: `ActivateRelease` testlerini yaz**

`iisswap_test.go`'nun sonuna ekle:

```go
func TestActivateRelease_NpmRecipeIsJustAPlainSwap(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeNpm, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if err != nil {
		t.Fatalf("ActivateRelease returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls, want 1 (npm recipe must never stop/start)", len(runner.calls))
	}
	if runner.calls[0][1] != "set" {
		t.Errorf("call = %v, want a plain 'set vdir'", runner.calls[0])
	}
}

func TestActivateRelease_DotnetRecipeStopsSwapsThenStarts(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if err != nil {
		t.Fatalf("ActivateRelease returned error: %v", err)
	}

	if len(runner.calls) != 3 {
		t.Fatalf("got %d calls, want 3 (stop, set, start): %v", len(runner.calls), runner.calls)
	}
	if runner.calls[0][1] != "stop" {
		t.Errorf("call[0] = %v, want stop site first", runner.calls[0])
	}
	if runner.calls[1][1] != "set" {
		t.Errorf("call[1] = %v, want set vdir second", runner.calls[1])
	}
	if runner.calls[1][len(runner.calls[1])-1] != `/physicalPath:C:\releases\v2` {
		t.Errorf("call[1] = %v, want it to target the new release", runner.calls[1])
	}
	if runner.calls[2][1] != "start" {
		t.Errorf("call[2] = %v, want start site third", runner.calls[2])
	}
}

func TestActivateRelease_DotnetRecipeRevertsOnStartFailure(t *testing.T) {
	runner := &fakeCommandRunner{failStart: []error{errors.New("simulated: new release crashes on start")}}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if !errors.Is(err, ErrReverted) {
		t.Fatalf("err = %v, want ErrReverted", err)
	}

	// stop, set(new), start(new, fails), set(previous), start(previous)
	if len(runner.calls) != 5 {
		t.Fatalf("got %d calls, want 5: %v", len(runner.calls), runner.calls)
	}
	if runner.calls[3][len(runner.calls[3])-1] != `/physicalPath:C:\releases\v1` {
		t.Errorf("call[3] = %v, want the revert swap to target the previous release", runner.calls[3])
	}
	if runner.calls[4][1] != "start" {
		t.Errorf("call[4] = %v, want a final start site to bring the previous release back up", runner.calls[4])
	}
}

func TestActivateRelease_DotnetRecipeReturnsSiteDownWhenRevertAlsoFails(t *testing.T) {
	runner := &fakeCommandRunner{failStart: []error{
		errors.New("simulated: new release crashes on start"),
		errors.New("simulated: previous release also fails to start"),
	}}
	swapper := NewIISSwapper(runner)

	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v2`, `C:\releases\v1`)
	if !errors.Is(err, ErrSiteDown) {
		t.Fatalf("err = %v, want ErrSiteDown", err)
	}
}

func TestActivateRelease_DotnetRecipeReturnsSiteDownImmediatelyWithNoPreviousRelease(t *testing.T) {
	runner := &fakeCommandRunner{failStart: []error{errors.New("simulated: first-ever deploy crashes on start")}}
	swapper := NewIISSwapper(runner)

	// previousReleaseDir is "" — there is nothing to fall back to (e.g.
	// the very first deploy for a target).
	err := swapper.ActivateRelease(RecipeDotnet, "DevPlatform Test Site", `C:\releases\v1`, "")
	if !errors.Is(err, ErrSiteDown) {
		t.Fatalf("err = %v, want ErrSiteDown", err)
	}
	// stop, set(new), start(new, fails) — no revert attempt possible.
	if len(runner.calls) != 3 {
		t.Fatalf("got %d calls, want 3 (no revert attempt without a previous release): %v", len(runner.calls), runner.calls)
	}
}
```

- [ ] **Step 3: Testleri çalıştır, başarısız olduklarını doğrula**

Run: `go test ./internal/deploy/... -run TestActivateRelease -v`
Expected: FAIL — derleme hatası, `ActivateRelease`/`ErrReverted`/`ErrSiteDown` yok.

- [ ] **Step 4: `ActivateRelease`'i ve sentinel hataları ekle**

`iisswap.go`'nun başına (import bloğundan sonra, `IISSwapper` struct'ından önce ya da `SetPhysicalPath`'in altına) ekle:

```go
// ErrReverted indicates a release failed to start but the previous,
// already-known-working release was successfully reactivated — the site
// is up and serving traffic, just not the version that was requested.
var ErrReverted = errors.New("deploy: release failed to start; reverted to the previous release")

// ErrSiteDown indicates a release failed to start AND the attempt to
// fall back to the previous release also failed (or there was no
// previous release to fall back to) — the site is genuinely down and
// needs manual intervention.
var ErrSiteDown = errors.New("deploy: site is down and could not be automatically recovered")
```

(`errors` paketi zaten `fmt` ile birlikte import edilmiş olmalı — değilse `"errors"`'ı import bloğuna ekle.)

`SetPhysicalPath`'in altına ekle:

```go
// ActivateRelease points siteName at newReleaseDir. For RecipeNpm this is
// exactly SetPhysicalPath — a static site has no running process to
// worry about. For RecipeDotnet, siteName is stopped before the swap and
// started after: a running process locks its own files, so a bare
// physical-path swap doesn't make it pick up newReleaseDir the way it
// does for a static site (confirmed against a real IIS-hosted process
// during this feature's design — see the spec). If starting the new
// release fails, this attempts to fall back to previousReleaseDir (the
// last known-good release) rather than leaving the site down; pass ""
// for previousReleaseDir when there is none (e.g. the first-ever deploy
// for a target), which skips straight to ErrSiteDown on start failure.
func (s *IISSwapper) ActivateRelease(recipe Recipe, siteName, newReleaseDir, previousReleaseDir string) error {
	if recipe != RecipeDotnet {
		return s.SetPhysicalPath(siteName, newReleaseDir)
	}

	if err := s.StopSite(siteName); err != nil {
		return err
	}
	if err := s.SetPhysicalPath(siteName, newReleaseDir); err != nil {
		return err
	}
	startErr := s.StartSite(siteName)
	if startErr == nil {
		return nil
	}

	if previousReleaseDir == "" {
		return fmt.Errorf("%w: %v", ErrSiteDown, startErr)
	}
	if err := s.SetPhysicalPath(siteName, previousReleaseDir); err != nil {
		return fmt.Errorf("%w: start failed (%v) and reverting the physical path also failed (%v)", ErrSiteDown, startErr, err)
	}
	if err := s.StartSite(siteName); err != nil {
		return fmt.Errorf("%w: start failed (%v) and restarting the previous release also failed (%v)", ErrSiteDown, startErr, err)
	}
	return fmt.Errorf("%w: %v", ErrReverted, startErr)
}
```

- [ ] **Step 5: Testleri tekrar çalıştır**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, tüm paket (Task 2'nin testleri dahil).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/deploy/iisswap.go backend/internal/deploy/iisswap_test.go
git commit -m "feat(deploy): add ActivateRelease with automatic revert on start failure"
```

---

### Task 4: `Pipeline.Deploy`'u `ActivateRelease`'e bağla

**Files:**
- Modify: `backend/internal/deploy/deploy.go`
- Modify: `backend/internal/deploy/deploy_test.go`

**Interfaces:**
- Consumes: Task 3'ün `IISSwapper.ActivateRelease`, `ErrReverted`.
- Produces: `Pipeline.Deploy`'un dönüş davranışı — `ErrReverted` durumunda `(previousReleaseDir, wrapped-err)` döner (boş string değil), diğer tüm hata durumlarında değişmedi.

- [ ] **Step 1: `releaseStore` arayüzüne `List` ekle, `fakePruneFailingStore`'u güncelle**

`deploy.go`'da:

```go
type releaseStore interface {
	NewRelease(repo, environment string) (string, error)
	Prune(repo, environment string, keep int) error
	List(repo, environment string) ([]string, error)
}
```

`deploy_test.go`'da `fakePruneFailingStore`'un altına ekle (arayüzü tekrar sağlaması için — `*VersionStore` zaten `List`'e sahip, sadece bu fake'e delege eden bir metod eksik):

```go
func (f *fakePruneFailingStore) List(repo, environment string) ([]string, error) {
	return f.real.List(repo, environment)
}
```

- [ ] **Step 2: Yeni testleri yaz**

`deploy_test.go`'nun sonuna ekle:

```go
func TestPipeline_Deploy_DotnetRecipeStopsAndStartsTheSite(t *testing.T) {
	requireTool(t, "dotnet")

	source, err := filepath.Abs("testdata/dotnet-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner), nil)

	_, err = pipeline.Deploy(source, RecipeDotnet, "sample", "test", "DevPlatform Test Site", 5, "")
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	if len(runner.calls) != 3 {
		t.Fatalf("got %d IIS calls, want 3 (stop, set, start): %v", len(runner.calls), runner.calls)
	}
	if runner.calls[0][1] != "stop" || runner.calls[2][1] != "start" {
		t.Errorf("calls = %v, want stop first and start last", runner.calls)
	}
}

func TestPipeline_Deploy_DotnetRecipeReturnsPreviousReleaseOnRevert(t *testing.T) {
	requireTool(t, "dotnet")

	source, err := filepath.Abs("testdata/dotnet-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())

	// First deploy succeeds normally, establishing a "previous release".
	firstRunner := &fakeCommandRunner{}
	firstPipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(firstRunner), nil)
	firstReleaseDir, err := firstPipeline.Deploy(source, RecipeDotnet, "sample", "test", "DevPlatform Test Site", 5, "")
	if err != nil {
		t.Fatalf("first Deploy returned error: %v", err)
	}

	// Second deploy's new release fails to start.
	secondRunner := &fakeCommandRunner{failStart: []error{errors.New("simulated: crashes on start")}}
	secondPipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(secondRunner), nil)
	releaseDir, err := secondPipeline.Deploy(source, RecipeDotnet, "sample", "test", "DevPlatform Test Site", 5, "")

	if !errors.Is(err, ErrReverted) {
		t.Fatalf("err = %v, want ErrReverted", err)
	}
	if releaseDir != firstReleaseDir {
		t.Errorf("releaseDir = %q, want the previous (still-live) release %q", releaseDir, firstReleaseDir)
	}
}
```

Not: `testdata/dotnet-fixture` zaten `TestBuild_Dotnet_ProducesOutput` (`build_test.go`) tarafından kullanılıyor — burada tekrar kullanılıyor, yeni bir fixture eklemeye gerek yok.

- [ ] **Step 3: Testleri çalıştır, başarısız olduklarını doğrula**

Run: `go test ./internal/deploy/... -run TestPipeline_Deploy_Dotnet -v`
Expected: FAIL — `Pipeline.Deploy` henüz `ActivateRelease`'i çağırmıyor, hâlâ her zaman `SetPhysicalPath` çağırıyor (stop/start hiç olmuyor).

- [ ] **Step 4: `Pipeline.Deploy`'u güncelle**

`deploy.go`'da `Deploy` metodunu şu şekilde değiştir:

```go
func (p *Pipeline) Deploy(sourceDir string, recipe Recipe, repo, environment, siteName string, keepVersions int, secretsTarget string) (string, error) {
	if keepVersions < 1 {
		return "", fmt.Errorf("deploy: keepVersions must be at least 1, got %d", keepVersions)
	}

	// Capture whatever's currently the newest release (if any) BEFORE
	// allocating a new one — RecipeDotnet's failure-recovery path in
	// IISSwapper.ActivateRelease needs a last-known-good release to fall
	// back to if the new version fails to start. Empty for a target's
	// first-ever deploy, which ActivateRelease treats as "no fallback
	// possible".
	existing, err := p.versions.List(repo, environment)
	if err != nil {
		return "", fmt.Errorf("deploy: failed to list existing releases: %w", err)
	}
	previousReleaseDir := ""
	if len(existing) > 0 {
		previousReleaseDir = existing[0]
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

	if err := p.iis.ActivateRelease(recipe, siteName, releaseDir, previousReleaseDir); err != nil {
		if errors.Is(err, ErrReverted) {
			// The site is back up, just on the previous release, not the
			// one that just failed — that's what's actually live now.
			return previousReleaseDir, fmt.Errorf("deploy: failed to activate release: %w", err)
		}
		return "", fmt.Errorf("deploy: failed to activate release: %w", err)
	}

	if err := p.versions.Prune(repo, environment, keepVersions); err != nil {
		return releaseDir, fmt.Errorf("%w: %v", ErrPruneFailed, err)
	}

	return releaseDir, nil
}
```

(`"errors"` paketi zaten import edilmiş olmalı; değilse import bloğuna ekle.)

- [ ] **Step 5: Testleri tekrar çalıştır**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, tüm paket (mevcut `TestPipeline_Deploy_BuildsVersionsAndSwaps` dahil — `npm` recipe olduğu için `ActivateRelease` hâlâ tek bir `SetPhysicalPath` çağrısına indirgeniyor, davranış değişmedi).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/deploy/deploy.go backend/internal/deploy/deploy_test.go
git commit -m "feat(deploy): route Pipeline.Deploy's IIS activation through ActivateRelease"
```

---

### Task 5: `deployment.Handlers` — yeni hata mesajlarını panelde göster

**Files:**
- Modify: `backend/internal/deployment/handlers.go`
- Test: `backend/internal/deployment/handlers_test.go`

**Interfaces:**
- Consumes: Task 4'ün `deploy.ErrReverted`, `deploy.ErrSiteDown`.
- Produces: `finishFailed` artık bir `liveReleaseDir string` parametresi alıyor; `failureReason` yeni iki mesajı tanıyor.

- [ ] **Step 1: `newTestHandlers`'ın kullandığı fixture'ı dotnet ile de deploy edilebilir hale getirmeye gerek yok** — bu test, gerçek appcmd yerine `fakeCommandRunner` kullanıyor, `ActivateRelease`'in dotnet dalı zaten `Task 3`'te izole test edildi. Burada sadece `Approve`'un `ErrReverted`/`ErrSiteDown`'ı doğru `FailureReason`'a çevirdiğini doğruluyoruz — gerçek bir dotnet build'e gerek yok, `Pipeline`'ı doğrudan sahte bir `releaseStore`+`IISSwapper` ile kurup `Handlers.Approve`'u çağırmak yeterli değil çünkü `Approve` kendi `Pipeline`'ını `Handlers.Pipeline` alanından alıyor — bu yüzden testte gerçek `newTestHandlers`'ı değil, `Pipeline.Deploy`'un davranışını zaten Task 4'te doğruladığımız için, burada sadece `failureReason` fonksiyonunu **doğrudan** (HTTP katmanı olmadan) test ediyoruz.

**Files (güncelleme):**
- Test: `backend/internal/deployment/deployment_test.go` (yeni bir `handlers_test.go` fonksiyonu değil, `failureReason` `handlers.go` içinde tanımlı ve aynı pakette olduğu için oraya birim testi eklemek yeterli — dosya adı önemli değil, `handlers_test.go`'ya ekliyoruz çünkü fonksiyon orada tanımlı.)

- [ ] **Step 2: Testleri yaz**

`handlers_test.go`'nun sonuna ekle:

```go
func TestFailureReason_RecognizesRevertedAndSiteDown(t *testing.T) {
	reverted := fmt.Errorf("deploy: failed to activate release: %w", deploy.ErrReverted)
	got := failureReason(stageDeploy, reverted)
	want := "Yeni versiyon başlatılamadı, otomatik olarak önceki çalışan versiyona dönüldü"
	if got != want {
		t.Errorf("failureReason(ErrReverted) = %q, want %q", got, want)
	}

	siteDown := fmt.Errorf("deploy: failed to activate release: %w", deploy.ErrSiteDown)
	got = failureReason(stageDeploy, siteDown)
	want = "Site durduruldu ve yeniden başlatılamadı — site şu an ERİŞİLEMEZ, elle müdahale gerekiyor"
	if got != want {
		t.Errorf("failureReason(ErrSiteDown) = %q, want %q", got, want)
	}
}
```

(`fmt` zaten `handlers_test.go`'da import edilmiş olabilir — değilse import bloğuna ekle.)

- [ ] **Step 3: Testi çalıştır, başarısız olduğunu doğrula**

Run: `go test ./internal/deployment/... -run TestFailureReason_RecognizesRevertedAndSiteDown -v`
Expected: FAIL — `failureReason` henüz bu iki mesajı üretmiyor, `strings.Contains` yoluyla "Deploy başarısız" gibi genel bir mesaja düşüyor.

- [ ] **Step 4: `failureReason`'ı güncelle**

`handlers.go`'da `failureReason` fonksiyonunu şu şekilde değiştir:

```go
func failureReason(stage deployStage, err error) string {
	switch stage {
	case stageCheckout:
		return "Checkout başarısız"
	case stageTimeout:
		return "Deploy zaman aşımına uğradı"
	}
	if errors.Is(err, deploy.ErrReverted) {
		return "Yeni versiyon başlatılamadı, otomatik olarak önceki çalışan versiyona dönüldü"
	}
	if errors.Is(err, deploy.ErrSiteDown) {
		return "Site durduruldu ve yeniden başlatılamadı — site şu an ERİŞİLEMEZ, elle müdahale gerekiyor"
	}
	// Everything past checkout comes back as one error from
	// deploy.Pipeline.Deploy, whose own wrapping (see its fmt.Errorf calls)
	// is the only thing consulted here — matching on the stage prefix it
	// added, never echoing the wrapped cause.
	msg := err.Error()
	switch {
	case strings.Contains(msg, "build failed"):
		return "Build başarısız"
	case strings.Contains(msg, "failed to activate release"):
		return "IIS yayınlama başarısız"
	case strings.Contains(msg, "secrets"):
		return "Secrets dosyası hazırlanamadı"
	default:
		return "Deploy başarısız"
	}
}
```

- [ ] **Step 5: `finishFailed`'e `liveReleaseDir` parametresi ekle**

`handlers.go`'da `finishFailed`'i şu şekilde değiştir:

```go
// finishFailed records a deploy attempt that started but failed, and
// responds 200 with the now-StatusFailed request. liveReleaseDir is
// normally "" (nothing is live from this attempt), but is the previous
// release's path when deployErr wraps deploy.ErrReverted — the site is
// still up, just not on what was requested, and the record should say so
// honestly instead of implying nothing is running.
//
// The raw error is logged server-side only. What reaches the panel and
// the author's notification is the short, stage-derived reason instead:
// deployErr can carry a build's entire stdout/stderr (secrets a build
// script printed included) or an appcmd argv full of absolute server
// paths, and FailureReason is visible to anyone with access to the repo.
func (h *Handlers) finishFailed(w http.ResponseWriter, repo, id string, req Request, stage deployStage, deployErr error, liveReleaseDir string) {
	log.Printf("deployment: deploy failed for %s/%s at stage %s: %v", repo, id, stage, deployErr)
	reason := failureReason(stage, deployErr)

	updated, storeErr := h.Store.Decide(repo, id, StatusFailed, liveReleaseDir, reason)
	if storeErr != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	_ = h.Audit.Log("system", audit.ActionDeploymentFailed, repo, id,
		"Deploy başarısız: "+repo+" → "+req.Environment+" ("+reason+")")
	h.notifyAuthor(req, "Deploy başarısız: "+repo+" → "+req.Environment+" — "+reason)

	writeJSON(w, http.StatusOK, updated)
}
```

`finishFailed`'in **her** çağrı yerini güncelle — `Approve` içinde tam olarak 2 yer var: checkout-dizini-oluşturma hatası ve pipeline sonucu hatası (`runPipeline`'ın kendisi `finishFailed`'i hiç çağırmıyor, sadece bir `pipelineResult` döndürüyor; timeout dahil her pipeline hatası `Approve`'daki ikinci çağrı noktasından geçiyor). Checkout hatası çağrısı `""` geçsin, pipeline hatası çağrısı `res.releaseDir` geçsin:

`Approve` metodunda:
```go
checkoutDir, err := os.MkdirTemp(h.CheckoutRoot, "checkout-*")
if err != nil {
	log.Printf("deployment: failed to create checkout dir under %q for %s/%s: %v", h.CheckoutRoot, repo, id, err)
	h.finishFailed(w, repo, id, req, stageCheckout, err, "")
	return
}
defer os.RemoveAll(checkoutDir)

res := h.runPipeline(gitRepo, req, target, checkoutDir)
if res.err != nil && !errors.Is(res.err, deploy.ErrPruneFailed) {
	h.finishFailed(w, repo, id, req, res.stage, res.err, res.releaseDir)
	return
}
```

(Not: `res.releaseDir`, Task 4 sayesinde `ErrReverted` durumunda otomatik olarak önceki release'in yolunu taşıyor — `stageTimeout` durumunda `pipelineResult{stage: stageTimeout, err: ...}` hâlâ `releaseDir` alanını hiç set etmiyor, yani zaten `""` — ayrıca dokunmaya gerek yok.)

- [ ] **Step 6: Testleri tekrar çalıştır**

Run: `go test ./internal/deployment/... -v`
Expected: PASS, tüm paket.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/deployment/handlers.go backend/internal/deployment/handlers_test.go
git commit -m "feat(deployment): surface auto-revert and site-down as distinct failure reasons"
```

---

### Task 6: `Rollback` — process tabanlı hedeflerde `ActivateRelease` kullan

**Files:**
- Modify: `backend/internal/deployment/rollback_handlers.go`
- Test: `backend/internal/deployment/rollback_handlers_test.go`

**Interfaces:**
- Consumes: Task 3'ün `ActivateRelease`, `ErrReverted`, `ErrSiteDown`; mevcut `activeRelease` (aynı dosyada zaten tanımlı).
- Produces: `Rollback` handler'ının davranışı — `target.Recipe == deploy.RecipeDotnet` olduğunda durdur→değiştir→başlat(+otomatik geri dönüş) uygular; hata durumunda **hiçbir zaman** `CreateRollback` çağırmaz (sadece gerçekten başarılı bir geçiş kayıt altına alınır).

- [ ] **Step 1: Testleri yaz**

`rollback_handlers_test.go`'nun sonuna ekle (bu testler `newTestHandlers`'ın `npm` recipe'iyle kurduğu `sample/test` hedefini kullanamaz — dotnet'e özgü davranışı izole test etmek için doğrudan `Handlers` kurup sahte bir dotnet hedefi tanımlıyoruz):

```go
func newDotnetTestHandlers(t *testing.T, runner deploy.CommandRunner) *Handlers {
	t.Helper()
	dataDir := t.TempDir()

	targets := NewTargetStore(filepath.Join(dataDir, "deploy-targets.json"))
	if err := targets.Set(
		Target{Repo: "backend-sample", Environment: "prod", Recipe: deploy.RecipeDotnet, SiteName: "Fake Backend Site"},
		map[string]bool{"Fake Backend Site": true},
	); err != nil {
		t.Fatalf("failed to seed dotnet deploy target: %v", err)
	}

	versions := deploy.NewVersionStore(filepath.Join(dataDir, "releases"))
	store := NewStore(filepath.Join(dataDir, "deployments"))

	// Two releases already on disk, as if two ordinary deploys already
	// happened — Rollback only ever operates on releases Versions.List
	// already reports, it never creates one.
	first, err := versions.NewRelease("backend-sample", "prod")
	if err != nil {
		t.Fatalf("failed to seed first release: %v", err)
	}
	if _, err := store.CreateRollback("backend-sample", "prod", first, "system"); err != nil {
		t.Fatalf("failed to record first release as active: %v", err)
	}
	second, err := versions.NewRelease("backend-sample", "prod")
	if err != nil {
		t.Fatalf("failed to seed second release: %v", err)
	}
	if _, err := store.CreateRollback("backend-sample", "prod", second, "system"); err != nil {
		t.Fatalf("failed to record second release as active: %v", err)
	}

	return &Handlers{
		Store:    store,
		Repos:    repostore.New(dataDir),
		Targets:  targets,
		Versions: versions,
		IIS:      deploy.NewIISSwapper(runner),
		Audit:    audit.New(filepath.Join(dataDir, "audit.jsonl")),
	}
}
```

Not: `repostore.New(dataDir)` burada gerçek bir git reposu olmadan çağrılıyor — `Handlers.repoExists("backend-sample")` bunu `Repos.List()` üzerinden kontrol ediyor, ama `backend-sample` diye bir bare repo hiç oluşturulmadı. Bu yüzden `Releases`/`Rollback` handler'larını doğrudan (mux üzerinden değil, fonksiyon olarak) çağırmak yerine, `repoExists` kontrolünü atlatmak için testte `httptest.NewRequest` + doğrudan `h.Rollback(rec, req)` çağırmadan önce şu satırı da ekle:

```go
if _, err := repostore.New(dataDir).Create("backend-sample"); err != nil {
	t.Fatalf("failed to create bare repo: %v", err)
}
```

Bunu `newDotnetTestHandlers`'ın en başına, `targets := ...` satırından önce ekle.

Şimdi asıl testler:

```go
func TestRollback_DotnetRecipeStopsSwapsThenStarts(t *testing.T) {
	runner := &fakeCommandRunner{}
	h := newDotnetTestHandlers(t, runner)
	mux := newMux(h)

	releases, err := h.Versions.List("backend-sample", "prod")
	if err != nil || len(releases) != 2 {
		t.Fatalf("expected 2 seeded releases, got %v (err: %v)", releases, err)
	}
	olderRelease := filepath.Base(releases[1]) // List is newest-first

	body, _ := json.Marshal(map[string]string{"release": olderRelease})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/backend-sample/deployments/prod/rollback", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(runner.calls) != 3 {
		t.Fatalf("got %d IIS calls, want 3 (stop, set, start): %v", len(runner.calls), runner.calls)
	}
	if runner.calls[0][1] != "stop" || runner.calls[2][1] != "start" {
		t.Errorf("calls = %v, want stop first and start last", runner.calls)
	}
}

func TestRollback_DotnetRecipeDoesNotRecordAnythingWhenStartFails(t *testing.T) {
	runner := &fakeCommandRunner{failStart: []error{errors.New("simulated: target release crashes on start")}}
	h := newDotnetTestHandlers(t, runner)
	mux := newMux(h)

	releases, err := h.Versions.List("backend-sample", "prod")
	if err != nil || len(releases) != 2 {
		t.Fatalf("expected 2 seeded releases, got %v (err: %v)", releases, err)
	}
	olderRelease := filepath.Base(releases[1])

	before, err := h.Store.List("backend-sample")
	if err != nil {
		t.Fatalf("Store.List failed: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"release": olderRelease})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/backend-sample/deployments/prod/rollback", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	after, err := h.Store.List("backend-sample")
	if err != nil {
		t.Fatalf("Store.List failed: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("got %d requests after a failed rollback, want unchanged %d — a failed rollback must never be recorded as one", len(after), len(before))
	}
}
```

- [ ] **Step 2: Testleri çalıştır, başarısız olduklarını doğrula**

Run: `go test ./internal/deployment/... -run TestRollback_Dotnet -v`
Expected: `TestRollback_DotnetRecipeStopsSwapsThenStarts` FAIL (şu an `Rollback` her zaman düz `SetPhysicalPath` çağırıyor, stop/start hiç olmuyor — 3 değil 1 çağrı bekleniyor). `TestRollback_DotnetRecipeDoesNotRecordAnythingWhenStartFails` da muhtemelen FAIL (şu an başlatma diye bir kavram yok, dolayısıyla "başarısız" senaryosu hiç tetiklenmiyor, test muhtemelen 500 yerine 200 görüp patlayacak).

- [ ] **Step 3: `Rollback` handler'ını güncelle**

`rollback_handlers.go`'da `Rollback` fonksiyonunun `h.IIS.SetPhysicalPath(...)` çağrısından itibaren olan kısmını şu şekilde değiştir:

```go
	reqs, err := h.Store.List(repo)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	currentlyActive := activeRelease(reqs, environment)

	if err := h.IIS.ActivateRelease(target.Recipe, target.SiteName, releaseDir, currentlyActive); err != nil {
		log.Printf("deployment: rollback failed for %s/%s to %q: %v", repo, environment, releaseDir, err)
		switch {
		case errors.Is(err, deploy.ErrSiteDown):
			http.Error(w, "500 rollback failed and the site is now DOWN — manual intervention required", http.StatusInternalServerError)
		case errors.Is(err, deploy.ErrReverted):
			http.Error(w, "500 rollback failed — that release could not be started, the site continues running its previous version", http.StatusInternalServerError)
		default:
			http.Error(w, "500 rollback failed", http.StatusInternalServerError)
		}
		return
	}

	created, err := h.Store.CreateRollback(repo, environment, releaseDir, user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	_ = h.Audit.Log(user.Subject, audit.ActionDeploymentRollback, repo, created.ID,
		"Rollback: "+repo+" → "+environment+" ("+filepath.Base(releaseDir)+")")

	writeJSON(w, http.StatusOK, created)
}
```

(Bu, eski `if err := h.IIS.SetPhysicalPath(target.SiteName, releaseDir); err != nil { ... }` bloğunun **tamamının yerini alıyor** — `os.Stat` kontrolünden hemen sonra, `created, err := h.Store.CreateRollback(...)` satırına kadar olan her şeyi değiştiriyorsun. `errors` paketi dosyada zaten import edilmiş olmalı; değilse ekle.)

- [ ] **Step 4: Testleri tekrar çalıştır**

Run: `go test ./internal/deployment/... -v`
Expected: PASS, tüm paket.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/deployment/rollback_handlers.go backend/internal/deployment/rollback_handlers_test.go
git commit -m "feat(deployment): use ActivateRelease for process-based rollback targets"
```

---

### Task 7: Tüm paketi baştan sona doğrula

**Files:** yok (sadece doğrulama).

- [ ] **Step 1: Tüm backend'i derle**

Run: `go build ./...`
Expected: hatasız.

- [ ] **Step 2: Tüm backend testlerini çalıştır**

Run: `go test ./...`
Expected: tüm paketler `ok`.

- [ ] **Step 3: `devplatform.exe`'yi yeniden derle**

Run: `go build -o ../dist/devplatform.exe ./cmd/devplatform` (backend klasöründen)
Expected: hatasız — sunucuya koyup canlı test etmeye hazır hale gelir (canlı test bu planın kapsamında değil, ayrı bir adım).

- [ ] **Step 4: Commit gerekmez** — bu görev sadece doğrulama, kod değişikliği yok.
