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
| Bildirim (panel içi + e-posta) | ❌ **Sıradaki iş** |

### Faz 2 — Otomasyon

Hiç başlanmadı: build/deploy otomasyonu, secrets deposu, versiyonlu
release + rollback.

### Faz 3 — Genişleme

Hiç başlanmadı: kişi ekleme/davet akışı, proje bazlı yetkilendirme,
gecelik yedekleme. (Kişi *kaydı* var ama davet/yetkilendirme yok.)

## Sıradaki iş: bildirimler

Faz 1'in kalan tek maddesi. Tasarım dokümanı şunu istiyor: panel içi
bildirim + e-posta; tetikleyiciler yeni görev ataması, onay bekleyen
talep, deploy sonucu.

Artık yapılabilir durumda çünkü kişi kaydı (`internal/users`) alıcıyı
biliyor. Yapılacaklar:

1. `internal/notify`: kullanıcı başına bildirim listesi (dosya tabanlı,
   `internal/taskboard` deseniyle aynı), okundu işaretleme.
2. Tetikleyicileri bağla: görev ataması → atanana; merge isteği açılması
   → Yönetici rolündeki kişilere (kayıttan bulunur).
3. `GET /api/notifications`, `POST /api/notifications/{id}/read`.
4. Frontend: üst barda okunmamış sayacı + bildirim listesi.
5. E-posta: SMTP ayarları config'e placeholder olarak eklenir
   (JWT secret'ta yaptığımız gibi), gerçek gönderim arayüz arkasına alınır.

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

## Kontroller

```bash
cd backend  && go build ./... && go vet ./... && go test ./...
cd frontend && npm run build && npm run lint
```

Backend testleri git CLI'ye ihtiyaç duyar (gerçek `git push`/`clone` ile
uçtan uca çalışırlar); git yoksa o testler atlanır.
