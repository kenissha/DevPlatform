# DevPlatform — Nerede Kaldık

Son güncelleme: 2026-08-12

Bu dosya, projeye ara verip geri döndüğünde "ne bitti, ne kaldı, nasıl
çalıştırırım" sorularının tek cevap yeri. Tasarımın tamamı için
[`superpowers/specs/2026-08-07-dev-platform-design.md`](superpowers/specs/2026-08-07-dev-platform-design.md).

## Nasıl çalıştırılır

İki süreç var; ikisini de ayrı terminalde başlat.

**Backend** (`:8080`):

```bash
cd backend
go run ./cmd/devplatform
```

Varsayılan olarak `./data` klasörünü kullanır. Ortam değişkenleriyle
değiştirilebilir: `DEVPLATFORM_DATA_DIR`, `DEVPLATFORM_LISTEN_ADDR`,
`DEVPLATFORM_GIT_USERNAME`, `DEVPLATFORM_GIT_PASSWORD`,
`DEVPLATFORM_JWT_SECRET`.

**Frontend** (`:5173`):

```bash
cd frontend
npm install   # ilk seferde
npm run dev
```

Dev sunucusu `/api` ve `/healthz` isteklerini `:8080`'e proxy'ler, yani
CORS ayarı yok — üretimde de aynı origin varsayılıyor.

### Giriş yapmak

Gerçek giriş, kurumun mevcut sisteminden gelen bir JWT ile olacak (bkz.
"Kimlik doğrulama"). Lokalde test için token üretmek gerekiyor: HS256 ile,
`DEVPLATFORM_JWT_SECRET` (varsayılan `dev-not-a-real-secret`) kullanarak,
şu claim'lerle: `sub`, `email`, `role` (`admin` | `developer`), `exp`.

Ürettiğin token'ı iki şekilde kullanabilirsin:

- `http://localhost:5173/?token=<JWT>` adresine git (SSO devrini taklit eder), veya
- `/login` sayfasındaki kutuya yapıştır.

### Git ile kullanmak

```bash
git remote add origin http://dev:dev@localhost:8080/git/<repo-adi>.git
git push origin <branch>
```

Kullanıcı/şifre `DEVPLATFORM_GIT_USERNAME` / `_PASSWORD` ile ayarlanır.
`main`'e doğrudan push **reddedilir** — merge isteği açman gerekir.

## Durum

### Faz 1 — Temel

| Parça | Durum |
|---|---|
| Git sunucusu (smart HTTP, go-git) | ✅ Bitti |
| Branch koruma (protokol seviyesinde) | ✅ Bitti |
| Push anında secret taraması | ✅ Bitti |
| Görev/talep panosu | ✅ Bitti |
| Merge talebi + diff inceleme ekranı | ✅ Bitti (onaylayınca gerçekten birleştiriyor) |
| Audit log | ✅ Bitti |
| Kimlik doğrulama & roller | ✅ Bitti (JWT devri; gerçek AD bağlantısı sende) |
| Kişi kaydı (assignee listesi) | ✅ Bitti (girişte otomatik kaydolur) |
| Bildirim (panel içi; e-posta yer tutucu) | ✅ Bitti (2026-08-12) |

**Faz 1 tamamlandı.** Tek eksik: e-posta gönderimi gerçek SMTP'ye
bağlanmadı — `internal/notify.EmailSender` arayüzü ve config'teki
`DEVPLATFORM_SMTP_*` alanları hazır ama kasıtlı olarak yer tutucu
(`NoopEmailSender` sadece loglar). Gerçek gönderim bağlanana kadar bu
şekilde kalacak.

### Faz 2 — Otomasyon

| Parça | Durum |
|---|---|
| Build + versiyonlu klasör + IIS swap (temel mekanizma) | ✅ Bitti (2026-08-12), gerçek IIS'e karşı kanıtlandı |
| Secrets deposu (gerçek appsettings enjeksiyonu) | ❌ Başlanmadı |
| Onay akışına bağlama (panelden tetikleme) | ❌ Başlanmadı |
| Gerçek Intranet-F/Intranet-B'ye bağlanma | ❌ Başlanmadı — bilinçli olarak sonraya bırakıldı |

`internal/deploy` paketi (`Builder`, `VersionStore`, `IISSwapper`,
`Pipeline`) hazır ve test edilmiş, ama şimdilik hiçbir HTTP endpoint'ine
bağlı değil — sadece `cmd/deploydemo` adlı bağımsız bir deneme aracından
çağrılabiliyor.

**2026-08-12 güncelleme:** Son incelemede bulunan 2 önemli not kapatıldı —
`copyDir` artık recursive (alt klasörleri de kopyalıyor, `TestBuild_Npm_ProducesOutput`
nested dosya testiyle kanıtlı), ve `Pipeline.Deploy` artık `Prune` hatasını
gerçek deploy hatasından ayırt edebiliyor (`ErrPruneFailed`, `errors.Is` ile
yakalanabilir, `releaseDir` yine de dönüyor çünkü site gerçekten güncellendi).

Diğer küçük notlar (acil değil): `Deploy`'da henüz `context.Context` yok
(ileride iptal/timeout gerekecek), `appcmdPath()` 64-bit varsayıyor.

### Faz 3 — Genişleme

Hiç başlanmadı: kişi ekleme/davet akışı, proje bazlı yetkilendirme,
gecelik yedekleme. (Kişi *kaydı* var ama davet/yetkilendirme yok.)

## Sıradaki iş

Faz 1 bitti, Faz 2'nin temel mekanizması (build+deploy+rollback) da
gerçek IIS'e karşı kanıtlanmış durumda. Sırada üç seçenek var: (a) Faz
2'nin devamı — secrets deposu + panelden tetikleme + gerçek Intranet'e
bağlanma (yukarıdaki 2 önemli notu önce ele alarak), (b) gerçek SMTP
gönderimini bağlamak, (c) bekleyen küçük iyileştirme notlarına bakmak
(aşağıdaki "Bilinmesi gereken kararlar" ve
`docs/superpowers/plans/2026-08-12-notifications.md`'nin son inceleme
notlarındaki Minor bulgular — hiçbiri engelleyici değil).

## Bilinmesi gereken kararlar

- **Kimlik doğrulama AD'ye doğrudan bağlanmıyor.** Gerçek giriş, senin
  mevcut sisteminde yapılıyor; buraya imzalı bir JWT devrediliyor.
  Değiştirilmesi gereken tek yer `backend/internal/auth/auth.go` —
  dosyanın başındaki yorumda tam olarak neyin ayarlanacağı yazıyor
  (algoritma HS256 varsayılıyor, rol `role` claim'inden okunuyor).
- **`main`'e ilk commit merge isteğiyle girer.** Yeni repo boşken `main`
  ref'i yoktur ve doğrudan push kapalıdır; hedefi `main` olan bir merge
  isteği onaylandığında branch o an oluşturulur.
- **Merge sadece fast-forward.** Kullandığımız go-git alfa sürümünde
  gerçek 3-way merge yok. Branch'ler ayrışmışsa istek 409 ile reddedilir
  ve açık kalır; o durumda git CLI ile elle birleştirmek gerekir.
- **Audit log yalnızca eklenir.** Dosya hiçbir zaman yeniden yazılmaz.
  Dosya sistemine erişimi olan biri yine de düzenleyebilir; gerçek
  kurcalama kanıtı (hash zinciri / dış sunucu) kapsam dışı bırakıldı.
- **Veri tabanı yok.** Her şey `DEVPLATFORM_DATA_DIR` altında düz dosya:
  bare git repoları, `tasks/`, `merge-requests/`, `users.json`,
  `audit.jsonl`. 2 kişilik ekip için kasıtlı bir tercih; büyürse
  taşınması gereken ilk yer burası.
- **Frontend'in backend binary'sine gömülmesi henüz yapılmadı.**
  Tasarımda var (tek dosya deploy), şu an iki ayrı süreç.

## IIS / deploy — canlıda öğrenilen dersler (2026-08-12)

Faz 2'nin temel mekanizmasını (`internal/deploy`: build → versiyonlu klasör
→ `appcmd` ile IIS swap) bu makinede gerçek IIS'e karşı kanıtladık
(`cmd/deploydemo`). Yol boyunca çıkan, koda değil ortama ait 3 gerçek engel:

- **`appcmd.exe` PATH'te değil.** IIS kurulumu `inetsrv`'i PATH'e eklemiyor.
  Kod artık `%SystemRoot%\System32\inetsrv\appcmd.exe`'yi doğrudan
  kullanıyor (`internal/deploy/iisswap.go`'daki `appcmdPath()`), PATH'e
  güvenmiyor — bu düzeltildi, tekrar karşılaşılmayacak.
- **IIS'e verilen fiziksel yol mutlak olmalı.** Göreli yol verilirse appcmd
  hatasız döner ama site 404 verir. `IISSwapper.SetPhysicalPath` artık
  mutlak olmayan yolu reddediyor (kod tarafı düzeltildi).
- **IIS içeriği kullanıcı profili altında olmamalı.** `C:\Users\<kullanıcı>\...`
  altındaki bir klasörü IIS'in anonim kimliği okuyamıyor (401.3/500.19) —
  `icacls ... /grant IIS_IUSRS:(OI)(CI)RX` kısmi çözdü ama üst klasörlerden
  biri (muhtemelen `Desktop` ya da kullanıcı profili) hâlâ engelliyordu.
  **Kalıcı çözüm, kod değil, konum:** IIS içeriği/versiyon klasörleri
  `C:\inetpub\...` gibi, kullanıcı profilinin dışında bir yerde tutulmalı —
  gerçek Intranet'e bağlanırken deploy verisinin kök klasörü buna göre
  seçilmeli (`DEVPLATFORM_DATA_DIR` ya da deploy'a özel ayrı bir kök).

## Kontroller

```bash
cd backend  && go build ./... && go vet ./... && go test ./...
cd frontend && npm run build && npm run lint
```

Backend testleri git CLI'ye ihtiyaç duyar (gerçek `git push`/`clone` ile
uçtan uca çalışırlar); git yoksa o testler atlanır.
