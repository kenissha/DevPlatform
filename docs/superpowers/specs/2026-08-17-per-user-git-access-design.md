# Kişi Başına Git Erişimi — Tasarım Dokümanı

Tarih: 2026-08-17

## Amaç ve Problem

Git'e (`clone`/`push`) erişim, tek bir paylaşılan kullanıcı adı/şifreyle
(`DEVPLATFORM_GIT_USERNAME`/`_PASSWORD`) çalışıyor — herkes aynısını
kullanıyor. Panelde bir kişiyi belirli repolarla sınırlasan (`internal/access`)
bile, o kişi git'e doğrudan bu paylaşılan şifreyle bağlanıp **her repoya**
erişebiliyor, çünkü git sunucusu isteği yapanın **kim olduğunu hiç bilmiyor**
— sadece "şifre doğru mu" diye bakıyor. Bu, projenin var oluş sebebini
(ikinci bir mühendisi tam erişim vermeden işe alabilmek) geçersiz kılıyor.

Hedef: her kullanıcının **kendine ait, birbirinden farklı** bir git anahtarı
olması; git sunucusunun isteği yapanın kimliğini bilmesi; panelde zaten var
olan repo-erişim kuralının (`access.Store.CanAccess`) git seviyesinde de
**aynen** uygulanması — git için ayrı bir yetki sistemi icat edilmiyor.

## Kapsam Dışı

- **SSH tabanlı git erişimi** — düşünüldü, kasıtlı olarak reddedildi. Yeni
  bir sunucu bileşeni (SSH sunucusu, ayrı port, ayrı kimlik doğrulama)
  gerektiriyor; bu, projenin şu anki ölçeğine (2 kişilik ekip) göre
  orantısız bir karmaşıklık/saldırı-yüzeyi artışı. İleride ayrı bir tasarım
  olarak ele alınabilir.
- **Git için okuma/yazma ayrımı** (görebilir ama push edemez gibi) —
  kasıtlı olarak yok. Panelde "görebiliyor musun" kuralı ne diyorsa, git'te
  de aynısı geçerli: göremiyorsa hiç erişemez, görebiliyorsa hem
  clone hem push edebilir. Basitlik tercih edildi.
- **Intranet-B/F'ye dokunmak** — gerekmiyor. DevPlatform zaten JWT'den
  kimin giriş yaptığını biliyor (İntranet SSO üzerinden), git anahtarı
  üretimi tamamen DevPlatform'un kendi içinde, panelden yapılan bir eylem.
- **Birden fazla anahtar / adlandırılmış anahtarlar** (GitHub'daki gibi
  "laptop için", "ev bilgisayarı için" ayrı ayrı) — kasıtlı olarak yok.
  Kişi başına **tek** aktif anahtar; yenisini üretmek eskisini geçersiz
  kılar (şifre sıfırlamaya benzer, basit tutuldu).

## Genel Mimari

Yeni bir paket, `internal/gittoken`, kişi başına tek bir anahtarın
**hash'ini** saklıyor (ham anahtar hiçbir zaman diskte durmuyor — sadece
üretildiği an, bir kere, kullanıcıya gösteriliyor).

Git rotasının (`/git/...`) önündeki ara katman değişiyor:

**Şu an:** `gitauth.RequireBasicAuth(sabit şifre)` → git handler

**Yeni:** `gittoken` kimlik doğrulaması (anahtar → kimlik) → adresten repo
adı çıkarımı (`/git/{repo}.git/...`) → `access.Store.CanAccess(subject,
repo)` kontrolü (panelin zaten kullandığı fonksiyonun **aynısı**) → git
handler

`gitauth` paketi ve `DEVPLATFORM_GIT_USERNAME`/`_PASSWORD` tamamen
kaldırılıyor — geçiş dönemi/eski sistemle birlikte çalışma kasıtlı olarak
yok, çünkü paylaşılan şifre kalırsa düzeltmenin anlamı kalmıyor.

## Bileşenler

### `internal/gittoken` (yeni paket)

```go
// Store bir kullanıcı başına en fazla bir aktif git anahtarının hash'ini
// tutar. Ham anahtar hiçbir zaman kalıcı olarak saklanmaz.
type Store struct { ... }

func NewStore(path string) *Store

// Generate subject için yeni bir anahtar üretir (kriptografik olarak
// güvenli rastgele, 32 byte), hash'ini (SHA-256) kaydeder, önceki anahtarı
// geçersiz kılar, ham anahtarı döner. Bu dönüş değeri tek fırsat —
// fonksiyon bir daha aynı ham değeri üretemez/geri getiremez.
func (s *Store) Generate(subject string) (rawToken string, err error)

// Revoke subject'in anahtar kaydını siler (varsa). Kayıt yoksa hata değil.
func (s *Store) Revoke(subject string) error

// Verify subject'in sakladığı hash ile rawToken'ın hash'ini sabit zamanlı
// karşılaştırır. subject'in kaydı yoksa ya da eşleşmiyorsa false.
func (s *Store) Verify(subject, rawToken string) bool
```

Depolama: `<DataDir>/git-tokens.json`, `repostore`/`access`/`users` ile
aynı desende (basit JSON dosyası, atomik yazma).

### Git kimlik doğrulama + erişim ara katmanı

`internal/gittoken` içinde ya da `internal/gitserver` içinde (uygulama
planı kesinleştirecek) yeni bir `RequireTokenAndAccess(tokens *gittoken.Store,
access *access.Store, next http.Handler) http.Handler`:

1. `r.BasicAuth()` ile kullanıcı adı (= subject) ve şifre (= ham anahtar)
   okunur.
2. `tokens.Verify(subject, password)` — başarısızsa 401 (git istemcisinin
   beklediği `WWW-Authenticate` başlığıyla, `gitauth`'ta zaten olduğu gibi).
3. Adresten repo adı çıkarılır (`gitserver.Prefix` = `/git/` sabitine göre,
   `/git/{repo}.git/...` şeklinden `{repo}` ayrıştırılır — küçük, bağımsız
   bir yardımcı fonksiyon).
4. `access.CanAccess(subject, repo)` — yoksa 403.
5. Hepsi geçtiyse `next.ServeHTTP` (asıl git handler).

Zamanlama-saldırısı savunması (`gitauth`'ta zaten çözülmüş desen: her iki
karşılaştırma da kısa devre yapmadan, koşulsuz çalıştırılır) buraya da
taşınıyor.

### Yeni API'ler (`internal/server`)

- `POST /api/me/git-token` — `auth.RequireAuth` arkasında (rol şartı yok,
  herkes kendi anahtarını üretebilir). JWT'deki `sub`'ı kullanır, path
  parametresi yok — kimse başkası için anahtar üretemez. Cevap: `{"token":
  "..."}`  — bu, o ham değerin **döndüğü tek an**.
- Yönetici için mevcut `PUT/DELETE /api/access/{subject}` deseniyle
  tutarlı: `DELETE /api/git-token/{subject}` — `auth.RequireRole(auth.RoleAdmin,
  ...)` arkasında, başka birinin anahtarını iptal eder.

### Frontend

- Yeni sayfa: `HesabimPage.tsx` (rota: `/hesabim` ya da benzeri) —
  "Git anahtarı oluştur/yenile" butonu; üretilince anahtar bir kartta,
  kopyala butonuyla, **"bir daha gösterilmeyecek"** uyarısıyla gösterilir;
  altında örnek `git clone http://<kullanıcı>:<anahtar>@<sunucu>/git/<repo>.git`
  komutu.
- `AccessPage.tsx`'e küçük ekleme: mevcut kişi listesindeki her satıra
  "Git anahtarını iptal et" butonu (sadece yönetici görür, sayfa zaten
  yönetici-only).

## Veri Akışı

**Anahtar üretme:** Kullanıcı → Hesabım sayfası → "Oluştur" → `POST
/api/me/git-token` → `gittoken.Store.Generate` → ham anahtar bir kerelik
cevapta döner → frontend gösterir, hiçbir yerde saklamaz.

**Git push/pull:** git istemcisi → `http://subject:token@sunucu/git/repo.git/...`
→ `RequireTokenAndAccess` → kimlik doğrulanır → repo adı çıkarılır →
erişim kontrol edilir → geçerse mevcut `gitserver` handler'ı (değişmedi).

**İptal:** Yönetici → Proje erişimi sayfası → "Git anahtarını iptal et" →
`DELETE /api/git-token/{subject}` → `gittoken.Store.Revoke` → o kişinin
bir sonraki git isteği 401 alır (yeni anahtar üretene kadar).

## Güvenlik

- Ham anahtar diskte **hiçbir zaman** durmuyor — sadece SHA-256 hash'i.
  Anahtar yüksek entropili rastgele veri olduğundan (insan şifresi değil),
  yavaş/salted hash (bcrypt gibi) gerekmiyor — her git isteğinde çalışacağı
  için hızlı, kriptografik bir hash (SHA-256) yeterli ve doğru tercih.
- Karşılaştırma sabit zamanlı (`subtle.ConstantTimeCompare`), `gitauth`'taki
  mevcut disiplinin aynısı.
- `POST /api/me/git-token`'da path parametresi yok — kimse başkasının
  anahtarını path'e subject yazarak üretemez, her zaman **çağıranın kendi**
  JWT kimliği kullanılır.
- Repo adı çıkarımı, `repostore`'un zaten uyguladığı isim doğrulamasına
  güveniyor (repo adları zaten `^[a-zA-Z0-9_-]+$` ile sınırlı) — adres
  ayrıştırmasında path-traversal riski yok, çünkü `access.CanAccess`'e
  giden değer sadece bir karşılaştırma anahtarı, dosya sistemi yolu değil.

## Test

- `gittoken.Store`: `Generate` sonrası `Verify` doğru anahtarla true, yanlış
  anahtarla false; `Generate` iki kere çağrılırsa eski anahtar artık
  geçersiz; `Revoke` sonrası her anahtar geçersiz.
- Yeni ara katman: token geçerli + erişim var → next çağrılır; token
  geçersiz → 401 (next çağrılmaz); token geçerli + erişim yok → 403 (next
  çağrılmaz); admin için erişim kontrolü her zaman geçer (mevcut
  `access.RequireRepoAccess` davranışıyla tutarlı).
- Gerçek git CLI ile uçtan uca (proje genelinde zaten kullanılan desen):
  gerçek bir anahtar üretip gerçek `git clone`/`push` ile deneme.

## Eski Sistemin Kaldırılması

- `internal/gitauth` paketi (dosyalar + testler) siliniyor.
- `config.GitUsername`/`GitPassword` alanları ve `DEVPLATFORM_GIT_USERNAME`/
  `_PASSWORD` ortam değişkenleri kaldırılıyor.
- `main.go`'daki git handler sarmalama satırı `gitauth.RequireBasicAuth(...)`
  yerine yeni `gittoken.RequireTokenAndAccess(...)` çağrısına dönüyor.
- `docs/DURUM.md`'ye bu geçiş not düşülüyor — mevcut paylaşılan şifreyle
  git kullanan biri varsa (şu an sadece biz), geçişten sonra herkesin
  Hesabım sayfasından yeni anahtar üretmesi gerekiyor.
