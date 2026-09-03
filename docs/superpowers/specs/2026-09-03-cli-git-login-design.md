# CLI Git Login — Design

## Amaç

Bugün git kimlik doğrulaması, panelden elle üretilen tek bir token'ın git
remote URL'sine kopyala-yapıştır edilmesiyle çalışıyor
(`backend/internal/gittoken`). İki gerçek sorun var:

1. **Kırılgan:** bir kişi (kendisi ya da biri) yeni bir token üretirse,
   eski token **sessizce** geçersiz oluyor — bu oturumda defalarca
   "neden 401 alıyorum" sürpriziyle sonuçlandı.
2. **Elle iş:** her yeni makine/klon için token'ı panelden kopyalayıp
   remote URL'sine yapıştırmak gerekiyor.

Bu spec, terminalden çalışan bir "login" akışı tanımlıyor: kullanıcı bir
CLI aracını bir kere kurar, bundan sonra git kendi kendine kimlik
doğrular — token'ı hiç görmeye/kopyalamaya gerek kalmaz, ve bir token
geçersiz olsa bile araç kendini otomatik yeniler.

## Kapsam dışı

- Tarayıcı tabanlı bir giriş akışı yok — kullanıcılar zaten geliştirici,
  terminalde kullanıcı adı/şifre girmek kabul edilebilir (kullanıcıyla
  netleştirildi).
- Intranet-B'de hiçbir değişiklik yok — mevcut `POST /api/auth/login` ve
  `POST /api/auth/devplatform-sso` uçları zaten düz JSON API, CLI'dan
  doğrudan çağrılabilir durumda (araştırıldı, doğrulandı).
- DevPlatform'un `POST /api/me/git-token` uç noktası değişmiyor — zaten
  geçerli herhangi bir DevPlatform JWT'sini (nasıl elde edildiğine
  bakmaksızın) kabul ediyor.
- Panel dışı IDE entegrasyonları (VS Code/JetBrains'in kendi git
  arayüzleri) ayrı bir konu — bu CLI aracı git'in kendi credential
  helper mekanizmasına kaydolduğu için onlar da otomatik faydalanır,
  ama ayrıca test edilmiyor bu spec kapsamında.

## 1. `gittoken.Store` — tek token yerine çoklu token

**Bugün:** `map[subject]string` (subject → tek hash). `Generate` her
çağrıda öncekini siliyor.

**Yeni:** `map[subject][]Token`, her `Token`:
```go
type Token struct {
	ID        string    `json:"id"`
	Hash      string    `json:"hash"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
}
```
- `Generate(subject, label string) (id, rawToken string, err error)` —
  **ekler**, öncekileri silmez. `label`, panelde hangi token'ın hangi
  makineye/amaca ait olduğunu göstermek için (örn. "CLI girişi -
  2026-09-03").
- `Revoke(subject, id string) error` — sadece o ID'li token'ı siler,
  idempotent (yoksa hata değil, `access.Store.Clear` deseniyle aynı).
- `List(subject string) ([]TokenInfo, error)` — `ID`, `Label`,
  `CreatedAt` döner (hash asla dışarı çıkmaz).
- `Verify(subject, token string) bool` — subject'in **tüm** token'larını
  sabit-zamanlı karşılaştırır, herhangi biri eşleşirse true.
- `RevokeAll(subject string) error` — subject'in **tüm** token'larını
  siler (idempotent, boş liste de dahil hata değil). Mevcut admin-only
  `DELETE /api/git-token/{subject}` rotası (`AccessPage.tsx`'teki "Git
  anahtarını iptal et" butonu) bugün tek token'ı siliyordu, artık bu
  metoda bağlanacak — "bu kişinin git erişimini tamamen kes" semantiği
  değişmiyor, sadece altında artık silinecek birden çok kayıt olabiliyor.

Panelin "Hesabım" sayfasındaki mevcut "Anahtar oluştur/iptal et" akışı,
tek token yerine bir liste + her satırda ayrı "İptal et" butonuna
dönüşür — API tarafında `GET /api/me/git-tokens` (liste),
`POST /api/me/git-token` (üret, artık `label` alıyor),
`DELETE /api/me/git-tokens/{id}` (kendi token'larından tek birini iptal
et — subject, oturumdaki kullanıcıdan gelir, path'te yer almaz).
`DELETE /api/git-token/{subject}` (admin-only, mevcut rota, artık
`RevokeAll` çağırıyor) değişmeden kalıyor.

## 2. Yeni CLI aracı — `backend/cmd/devplatform-login`

Git'in kendi ["credential helper"](https://git-scm.com/docs/gitcredentials)
protokolünü uygulayan küçük bir Go binary'si. Git, bir `https://` remote'a
her eriştiğinde bu aracı üç şekilde çağırır:

- **`get`** — stdin'den `protocol=https\nhost=git.sigortatahkim.org\n\n`
  gelir. Araç:
  1. Yerel önbellekte (bkz. aşağı) geçerli bir token varsa, stdout'a
     `username=<subject>\npassword=<token>\n` yazıp çıkar — **hiç soru
     sormadan**, sessizce.
  2. Yoksa (ilk kullanım ya da önbellek temizlenmiş), terminalin kendi
     giriş/çıkışını (stdin/stdout değil — onlar git protokolü için
     ayrılmış, Windows'ta `CONIN$`/`CONOUT$` üzerinden) kullanarak
     kullanıcı adı + şifre sorar, aşağıdaki 3 adımlı zinciri çalıştırır,
     sonucu önbelleğe yazar, stdout'a döner.
- **`store`** — git, kimlik doğrulama başarılı olunca çağırır. v1'de
  no-op (önbelleği zaten biz `get` sırasında dolduruyoruz).
- **`erase`** — git, kimlik doğrulama **başarısız** (401) olunca çağırır.
  Araç önbellekteki kaydı siler — bir sonraki `get` otomatik olarak
  yeniden login akışını tetikler. **Bu, "token sessizce ölür" sorununu
  kullanıcı hiç fark etmeden kendi kendine çözüyor:** biri panelden yeni
  bir token ürettiğinde (artık çoklu token olsa da, elle iptal edilirse)
  bir sonraki git işlemi otomatik olarak yeniden giriş ister, "neden 401
  alıyorum" sürprizi olmaz.

**Login zinciri (3 adım, hepsi düz HTTPS JSON çağrısı):**
1. `POST https://intranet.sigortatahkim.org/api/auth/login`
   `{"Username": "...", "Password": "..."}` → Intranet JWT
2. `POST https://intranet.sigortatahkim.org/api/auth/devplatform-sso`
   (Bearer: Intranet JWT) → DevPlatform JWT
3. `POST https://git.sigortatahkim.org/api/me/git-token`
   (Bearer: DevPlatform JWT, body: `{"label": "CLI - <hostname>"}`) →
   git token

Şifre sadece bellekte tutulur (Go `string` — GC'ye kalana kadar bellekte
kalır, diske asla yazılmaz), zincir bitince referans bırakılmaz.

**Yerel önbellek:** `%LOCALAPPDATA%\devplatform\credential` dosyası,
Windows DPAPI (`CryptProtectData`/`CryptUnprotectData`, `CURRENT_USER`
kapsamı) ile şifrelenmiş — sadece o Windows kullanıcı hesabı, o makinede
çözebiliyor. Başka bir makineye kopyalansa işe yaramaz (bu istenen bir
özellik: her makine kendi girişini yapar).

**Kurulum (bir kere):**
```
devplatform-login install
```
bu, `git config --global credential.https://git.sigortatahkim.org.helper "<tam-yol>\devplatform-login.exe"` çalıştırır. Kullanıcı bundan sonra
remote URL'lerini **token'sız** yazabilir:
```
git remote add origin https://git.sigortatahkim.org/git/<repo>.git
```

## 3. Dağıtım

`iishelper.exe`/`secretsctl.exe` ile aynı desen: `go build` ile
derlenir, geliştiricilere elden/bir paylaşım klasöründen dağıtılır — bu
spec'in kapsamında otomatik bir dağıtım mekanizması (örn. panelden
indirme linki) yok, YAGNI. İstenirse ayrı bir iş olarak eklenebilir.

## Güvenlik notları

- AD şifresi CLI'a giriliyor olması, tarayıcıdan Intranet'in kendi giriş
  sayfasına girmekle **aynı güven modeli** — şifre yine AD'ye
  (`AdAuthService`'in LDAP bind'i üzerinden) doğrudan gidiyor, üçüncü bir
  yerde saklanmıyor. Ama kullanıcıya açıkça söylenmeli: bu bir üçüncü
  parti CLI aracı, şifresini ona giriyor — kod açık olduğu için
  (DevPlatform'un kendi reposunda) denetlenebilir olması bu güveni
  destekliyor.
- Önbellekteki token, DPAPI ile bu makine + bu kullanıcıya bağlı — çalınan
  bir dizüstü bilgisayardan (aynı Windows oturumu açıksa) yine de
  kullanılabilir, ama bu zaten Windows'un kendi oturum güvenliğine
  dayanan, bu spesin çözmeye çalışmadığı bir tehdit modeli.
- `erase` çağrısının otomatik önbellek temizlemesi, kötü niyetli bir
  aktörün "token'ı sürekli geçersiz kılıp kurbanı sürekli login'e
  zorlama" (bir tür kaba DoS) riski taşımıyor — sadece o kişinin **kendi**
  git isteği 401 aldığında tetikleniyor, başkasının token'ını
  etkilemiyor.

## Test yaklaşımı

- `gittoken.Store`'un çoklu-token davranışı: mevcut `store_test.go`
  deseniyle (TDD, `t.TempDir()`), Generate/Revoke/List/Verify'ın çoklu
  token senaryolarını (iki token, birini iptal, diğeri hâlâ geçerli mi)
  kapsayan testler.
- `devplatform-login`'in git-credential-helper protokolü: stdin/stdout
  üzerinden gerçek protokol formatını (key=value satırları) doğrulayan
  birim testleri — gerçek bir git işlemine karşı uçtan uca test bu
  ortamda pratik değil (gerçek AD/Intranet-B gerektiriyor), o kısım
  gözetimli, elle doğrulanacak.
- DPAPI şifreleme/çözme: Windows'a özgü, `GOOS=windows` build tag'i
  altında, gerçek DPAPI çağrılarına karşı round-trip testi.

## Etkilenen dosyalar (özet)

- `backend/internal/gittoken/store.go` — çoklu token veri modeli
- `backend/internal/gittoken/handlers.go` — `List`, `label` alanı,
  `Revoke(subject, id)` imza değişikliği
- `backend/internal/server/server.go` — yeni rotalar
  (`GET /api/me/git-tokens`, `DELETE /api/me/git-tokens/{id}`)
- `frontend/src/pages/HesabimPage.tsx` — tek token yerine liste + her
  satırda iptal butonu
- `backend/cmd/devplatform-login/` (yeni) — CLI aracının tamamı: login
  zinciri, credential-helper protokolü, DPAPI önbellek, `install`
  alt-komutu
