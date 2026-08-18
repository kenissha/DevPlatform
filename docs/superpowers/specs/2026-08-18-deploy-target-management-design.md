# Deploy Hedeflerinin Panelden Yönetimi — Tasarım Dokümanı

Tarih: 2026-08-18

## Amaç ve Problem

Bir repo+ortam çiftinin nereye deploy olacağı (hangi IIS site'ı, hangi
build recipe'i, secrets dosyası nereye gidecek) şu an
`DEVPLATFORM_DEPLOY_TARGETS_FILE` diye elle düzenlenen bir JSON
dosyasında tanımlı. Bu dosya sunucu başlarken bir kere okunuyor
(`deployment.LoadTargets`), panelden hiç değiştirilemiyor — yeni bir
proje/ortam eklemek için birinin sunucuya elle dosya yazıp
`devplatform.exe`'yi yeniden başlatması gerekiyor.

Hedef: bir yönetici, panelden yeni bir deploy hedefi ekleyebilsin,
düzenleyebilsin, silebilsin — sunucuya elle dokunmadan.

## Kapsam Dışı

- **Proje (repo) oluşturma** — zaten panelden çalışıyor
  (`POST /api/repos`), bu tasarımın konusu değil.
- **Var olan bir projenin (örn. GitHub'daki bir repo) DevPlatform'a
  taşınması** — standart git ile zaten çözülü (`git remote add` +
  `git push`, kişi başına git anahtarıyla); yeni bir özellik
  gerektirmiyor.
- **`main`'e doğrudan push / yönetici için branch-protection bypass'ı**
  — kasıtlı olarak yok ve bu tasarımda da eklenmiyor; ilk commit her
  zaman onaylanan bir merge isteğiyle giriyor, admin dahil herkes için.
- **IIS site adları listesinin panelden yönetilmesi** — kasıtlı olarak
  dışarıda bırakıldı, bkz. "Güvenlik" bölümü. Bu liste sadece sunucuya
  elle dokunularak değişir.
- **`iishelper` için canlı yeniden yükleme (hot-reload)** — site listesi
  değiştiğinde `iishelper`'ın (ve `devplatform.exe`'nin) yeniden
  başlatılması gerekiyor; bu zaten nadir olacak bir olay (yeni bir IIS
  site'ı elle açıldığında), otomatikleştirmeye değmez.
- **Eski dosyadan veri göçü (migration)** — `DEVPLATFORM_DEPLOY_TARGETS_FILE`
  içinde şu an gerçek/canlı bir hedef tanımlı değil (bkz.
  `docs/DURUM.md`, "Sıradaki iş" — bu hâlâ yapılacaklar listesinde), o
  yüzden eski veriyi yeni sisteme aktaran bir göç adımı yok. Yönetici
  ilk hedefleri doğrudan yeni panel ekranından girecek.

## Genel Mimari

Şu an tek dosyanın yaptığı iki farklı iş ayrılıyor:

1. **Deploy hedefinin içeriği** (repo, environment, recipe, siteName,
   secretsTarget, keepVersions) — sık değişir, artık panelden
   yönetiliyor, `internal/deployment.Store` diye yeni bir depoda,
   `DataDir` altında (`access.Store`/`users.Store`/`gittoken.Store` ile
   aynı desende: JSON dosyası, atomik yazma, mutex).
2. **Hangi IIS site adlarına dokunulabileceği** — nadiren değişir,
   `DEVPLATFORM_ALLOWED_SITES_FILE` diye yeni, küçük, sadece site
   adlarının düz bir listesini içeren bir dosyada, hâlâ sunucuya elle
   yazılıyor. Ne panel ne de `devplatform.exe`'nin hiçbir API'si bu
   dosyaya asla yazmıyor — sadece okuyor.

Bir deploy hedefi panelden kaydedilirken, `siteName` alanı bu ikinci
listede yoksa reddediliyor. Böylece "DevPlatform'da Yönetici rolüne
sahip olmak", "sunucudaki dosyaya elle erişebilmek" ile aynı güven
seviyesine hiçbir zaman gelmiyor — panel sadece **önceden onaylı**
site'lar arasından seçim yaptırıyor, yeni bir site'ı asla kendi başına
icat edemiyor. `iishelper`'ın appcmd çalıştırma sınırı (`ValidateRequest`,
`LoadAllowedSites`) hiç değişmiyor; sadece bu sınırın kaynağı olan
dosyanın içeriği artık daha küçük ve tek amaçlı.

## Bileşenler

### `internal/deployment.Store` (mevcut `Targets`'ın yerini alıyor)

```go
// Store, deploy hedeflerini kalıcı, panelden düzenlenebilir bir JSON
// dosyası olarak tutar — access.Store ile aynı desen. Find/Environments
// imzaları mevcut Targets ile aynı kalıyor, böylece Handlers.Create ve
// Handlers.Environments hiç değişmeden yeni Store'u kullanmaya devam
// ediyor; sadece yazma tarafı (Set/Delete/List) yeni.
type Store struct { ... }

func NewStore(path string) *Store

// Find ve Environments: mevcut *Targets ile birebir aynı imza/davranış
// (bkz. internal/deployment/targets.go), sadece artık dosyadan her
// çağrıda okuyor, süreç başında bir kere değil.
func (s *Store) Find(repo, environment string) (Target, error)
func (s *Store) Environments(repo string) []string

// List, panelin admin tablosu için tüm hedefleri döner.
func (s *Store) List() ([]Target, error)

// Set, (repo, environment) anahtarıyla bir hedefi oluşturur ya da
// (varsa) yerine geçirir — access.Store.Set ile aynı "tekrar
// çağırınca üzerine yazar" idiomu. allowedSites'a göre doğrular
// (bkz. validateTarget); repo+environment map anahtarı olduğu için
// aynı çiftin iki kez tanımlanması yapısal olarak imkansız — eski
// ValidateTargets'ın çoklu-liste yinelenme kontrolüne artık gerek yok.
func (s *Store) Set(target Target, allowedSites map[string]bool) error

// Delete, (repo, environment) hedefini kaldırır. Kayıt yoksa hata değil
// (access.Store.Clear ile aynı davranış).
func (s *Store) Delete(repo, environment string) error
```

`ValidateTargets` (çoğul, tüm listeyi bir kerede doğrulayan eski
fonksiyon) kaldırılıyor; yerine tek bir hedefi doğrulayan
`validateTarget(t Target, allowedSites map[string]bool) error` geliyor
— eski `ValidateTargets`'ın alan bazlı kontrollerinin (repo/environment
format, recipe enum, secretsTarget path güvenliği) aynısı, artı yeni
"siteName allowedSites içinde mi" kontrolü.

### Site adı listesi

`internal/iishelper.LoadAllowedSites` kalıyor ama artık farklı, daha
basit bir dosya formatını okuyor — düz bir `[]string`
(`["intranet-backend-test", "intranet-frontend-test", ...]`), eski
`targetEntry{SiteName string}` çıkarma mantığına gerek kalmıyor. Hem
`cmd/iishelper/main.go` hem `cmd/devplatform/main.go` bu fonksiyonu
**bağımsız olarak** çağırıp aynı dosyayı okuyor — tıpkı bugün ikisinin
de aynı eski dosyayı bağımsız okuduğu gibi, aralarında hiçbir çalışma
zamanı iletişimi yok. `devplatform.exe` bunu hem `GET
/api/allowed-sites`'ı cevaplamak hem de `PUT /api/deploy-targets/...`
isteklerini doğrulamak için kullanıyor.

Ortam değişkeni adı `DEVPLATFORM_DEPLOY_TARGETS_FILE`'dan
`DEVPLATFORM_ALLOWED_SITES_FILE`'a değişiyor — geriye dönük uyumluluk
kasıtlı olarak yok (git-token geçişinde olduğu gibi, geçiş dönemi
kafa karıştırır). `internal/config.Config`'ten `DeployTargetsFile` alanı
tamamen kaldırılıyor, yerine `AllowedSitesFile string`
(`DEVPLATFORM_ALLOWED_SITES_FILE`) geliyor.

**Sonuç:** site listesi değiştiğinde (yeni bir IIS site'ı elle
açıldığında) hem `iishelper` hem `devplatform.exe` yeniden
başlatılmalı ki ikisi de yeni listeyi görsün — ikisi de kendi başına,
sadece bir kere, süreç başlarken okuyor.

### Yeni API'ler (`internal/deployment`, `internal/server`)

`/api/access/{subject}` üçlüsüyle birebir aynı desen:

- `GET /api/deploy-targets` — admin-only, `Store.List()`.
- `PUT /api/deploy-targets/{repo}/{environment}` — admin-only,
  gövdede `recipe`/`siteName`/`secretsTarget`/`keepVersions`; oluşturur
  ya da (varsa) yerine geçirir. `siteName` yüklü allow-list'te yoksa
  400.
- `DELETE /api/deploy-targets/{repo}/{environment}` — admin-only,
  kaldırır.
- `GET /api/allowed-sites` — admin-only, salt okunur; panelin site adı
  alanını serbest metin yerine bu listeden bir **dropdown** yapması
  için.

Mevcut `GET /api/repos/{repo}/deploy-targets` (environment isimlerini
döner, deploy isteği formu için) ve `POST/GET /api/repos/{repo}/deployments`
gibi geliştirici tarafı rotalar hiç değişmiyor.

### Frontend

Yeni admin sayfası, "Deploy Hedefleri" (`/deploy-targets`) — mevcut
"Proje erişimi" sayfasıyla aynı görsel desende:

- Tablo: repo, environment, recipe, site, secretsTarget, keepVersions,
  düzenle/sil butonları.
- Ekleme/düzenleme formu: **repo** mevcut repoların listesinden
  (`GET /api/repos`) seçilen bir dropdown (yazım hatasıyla hiçbir
  reponun eşleşmeyeceği bir hedef oluşturmayı önlemek için — mevcut
  repo listesi zaten panelde var, tekrar kullanmak bedava); **environment**
  serbest metin (sabit bir liste değil, sadece bir etiket, sunucu
  tarafında zaten regex ile doğrulanıyor); **recipe** dropdown
  (dotnet/npm); **site** dropdown (`GET /api/allowed-sites`); **secretsTarget**
  opsiyonel serbest metin; **keepVersions** sayı girişi.

## Veri Akışı

**Hedef ekleme:** Yönetici → Deploy Hedefleri sayfası → form doldurur
(site adı dropdown'dan) → `PUT /api/deploy-targets/{repo}/{environment}`
→ `validateTarget` (allowedSites dahil) → `Store.Set` → panel listeyi
yeniler.

**Deploy isteği açma (değişmiyor):** Geliştirici → repo'nun deploy
sayfası → environment seçer (`GET /api/repos/{repo}/deploy-targets`'tan
gelen liste) → `POST /api/repos/{repo}/deployments` → `Handlers.Create`
→ `Store.Find(repo, environment)` (aynı imza, artık dosyadan canlı okuyor).

**Onay (değişmiyor):** Yönetici onaylar → `deploy.Pipeline` çalışır →
`iishelper`'a appcmd isteği gider → `iishelper` kendi bağımsız yüklediği
allow-list'e göre doğrular. Bu adımda hiçbir şey değişmiyor.

## Güvenlik

- Site adları listesi panelden **hiçbir zaman** yazılamaz — sadece
  sunucuya elle dosya yazarak değişir. Bu, `iishelper`'ın var oluş
  sebebini (devplatform.exe ele geçirilse bile appcmd'nin sadece
  önceden onaylı site'lara dokunabilmesi) korur. Bu sınır olmasaydı,
  "DevPlatform'da Yönetici rolü" ile "sunucuda gerçek dosya erişimi"
  aynı güven seviyesine gelirdi — ki sunucu DevPlatform dışında başka
  canlı projeleri de barındırıyor (`docs/DURUM.md`).
- `PUT /api/deploy-targets/{repo}/{environment}` admin-only
  (`auth.RequireRole(auth.RoleAdmin, ...)`), `/api/access/{subject}`
  ile birebir aynı yetkilendirme deseni.
- `secretsTarget` doğrulaması (`filepath.IsLocal`) değişmiyor — zaten
  path-traversal'a kapalıydı.
- Bu özellik tek başına hiçbir deploy'u tetiklemiyor; sadece "hangi
  hedefler var" listesini değiştiriyor. Gerçek bir deploy hâlâ ayrı,
  değişmeyen bir akıştan geçiyor: geliştirici ister, yönetici onaylar,
  `iishelper` kendi sınırını kendi uygular.

## Test

- `deployment.Store`: `Set` sonrası `Find` aynı hedefi döner; `Set` iki
  kez aynı (repo,environment) ile çağrılırsa öncekinin yerine geçer
  (yinelenme değil); `Delete` sonrası `Find` `ErrNoTarget` döner;
  `Delete` var olmayan bir hedef için hata değil; `List` tüm hedefleri
  döner; `Set` allow-list'te olmayan bir `siteName` için reddeder;
  mevcut alan doğrulamaları (`ValidateTargets`'tan taşınan testler) —
  geçersiz repo/environment format, bilinmeyen recipe, boş siteName,
  path-traversal'lı secretsTarget.
- Yeni HTTP handler'lar: router seviyesinde admin-only testi (mevcut
  `TestAccess_ManagementAPIIsAdminOnly` deseniyle aynı) —
  geliştirici için 403, yönetici için başarılı; `GET /api/allowed-sites`
  admin-only.
- `iishelper.LoadAllowedSites`: yeni, basit `[]string` formatını doğru
  okuduğunu doğrulayan test (mevcut testler zaten var, format
  değiştiği için güncellenmesi gerekiyor).
- `Handlers.Create`/`Handlers.Environments`: mevcut testler `*Targets`
  yerine `*Store` kullanacak şekilde güncelleniyor, davranış (ve
  testlerin kendisi) değişmiyor.

## Eski Sistemin Kaldırılması

- `deployment.LoadTargets`, `deployment.Targets` (eski, değişmez tip),
  `deployment.ValidateTargets` (çoğul) kaldırılıyor.
- `internal/config.Config.DeployTargetsFile` kaldırılıyor, yerine
  `AllowedSitesFile` ekleniyor.
- `cmd/iishelper/main.go`: `DEVPLATFORM_DEPLOY_TARGETS_FILE` yerine
  `DEVPLATFORM_ALLOWED_SITES_FILE` okunuyor.
- `docs/DURUM.md`'ye bu geçiş not düşülüyor: gerçek sunucuda henüz
  hiçbir deploy hedefi tanımlanmadığı için göç gerekmiyor; "gerçek
  Intranet-F/Intranet-B'yi deploy hedefi olarak eklemek" artık panelden
  yapılacak, dosya elle düzenlenerek değil. Ayrıca yeni
  `DEVPLATFORM_ALLOWED_SITES_FILE`'ın içeriğinin (gerçek IIS site
  adları) sunucuda elle oluşturulması gerektiği not düşülüyor.
