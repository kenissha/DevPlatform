# STK Atölye Frontend Redesign — Design

## Amaç

DevPlatform'un React panelini hem görsel hem kullanım (etkileşim) açısından
baştan gözden geçirmek. Şu anki panel işlevsel ama kafa karıştırıcı: repo-içi
navigasyon global menüyle karışık duruyor, görev takibi düz bir liste (Jira'nın
en tanıdık özelliği olan board yok), ve platformun adı hâlâ jenerik
"DevPlatform" — kurumu (Sigorta Tahkim Komisyonu) yansıtmıyor.

Yön: **GitHub'ın bilgi mimarisi + Jira'nın görev takibi hissi**, mevcut renk/tema
sistemi (`frontend/src/index.css`) korunarak.

## Kapsam dışı

- Backend'de yeni bir iş mantığı yok — `TaskStatus` üç sabit değer
  (`in_progress`, `awaiting_test`, `done`) olarak kalıyor, dördüncü bir durum
  ("yapılacak/backlog") eklenmiyor: mevcut model buna izin vermiyor
  (`taskboard.Create` her zaman `StatusInProgress` ile başlatıyor) ve bu
  spec'in kapsamında bir backend değişikliği yok.
- Renk paleti/tema sistemi değişmiyor — `index.css`'teki `--accent`,
  `--success`, `--warn`, `--danger` token'ları olduğu gibi kullanılacak.
- Deploy Hedefleri, Denetim Kaydı, Bildirimler, Proje Erişimi sayfalarının
  temel işlevi/veri akışı değişmiyor — sadece görsel/bileşen tutarlılığı.
- Kimlik doğrulama (SSO/JWT) akışı değişmiyor.

## 1. İsimlendirme

- Uygulama adı her yerde **"STK Atölye"** olarak değişir:
  `AppLayout.tsx`'teki `<Link to="/" className="brand">` metni, `<title>`
  (varsa `index.html`), ve varsa başka statik metinler (README, panel
  içi yardım metinleri).
- Logo/ikon (`LogoMark`, `components/icons.tsx`) aynı kalır — bu spec'in
  kapsamında yeni bir logo tasarımı yok.

## 2. Ad-soyad gösterimi

Şu an `User` sadece `subject` ve `email` taşıyor (SSO JWT'sinde isim claim'i
yok — `backend/internal/auth/auth.go`). Yeni bir `internal/displaynames`
paketi eklenir:

- `Store` — `internal/access.Store` ile aynı desen: `subject → "Ad Soyad"`
  eşlemesini tek bir JSON dosyasında tutar, her çağrıda diskten taze okur.
- `Get(subject, fallbackEmail string) string` — bir eşleme varsa onu, yoksa
  `fallbackEmail`'i (bugünkü davranış) döner. Kademeli doldurma: hiç kimse
  için kayıt girilmemişse panel bugünkünden farksız çalışır.
- Panelde: **Proje Erişimi** sayfasına (zaten admin-only, kullanıcı bazlı bir
  sayfa) yeni bir "Görünen adlar" bölümü eklenir — subject listesi (mevcut
  `Person` kayıtlarından, `listPeople` zaten var) + her biri için düzenlenebilir
  bir ad-soyad alanı.
- `AppLayout.tsx`'teki `{user.email || user.subject}` gösterimi,
  `displayNames.Get(user.subject, user.email)`'in döndürdüğü değere döner —
  bu, `/api/me` yanıtına eklenecek yeni bir `displayName` alanı üzerinden
  frontend'e ulaşır.

## 3. Navigasyon — sidebar / top-tab ayrımı

**Bugün:** `AppLayout.tsx`'in sidebar'ı hem global sayfaları (Panel, Tüm
Repolar, Denetim Kaydı, Bildirimler, Hesabım, Proje Erişimi, Deploy
Hedefleri) hem de aktif repo'nun alt sayfalarını (Genel bakış, Görevler,
Merge İstekleri, Branch'ler, İstatistikler, Deploy) aynı listede, art arda
gösteriyor.

**Yeni:** İki seviye ayrılır:
- **Sol sidebar**: sadece global nav + repo listesi kalır (repo-içi
  `sidebar-group` bloğu kaldırılır).
- **Repo üst tab bar**: bir repo sayfasının (`/repos/:repo/*`) içine
  girildiğinde, `main` alanının üstünde yatay bir tab şeridi belirir — repo
  adı + Genel bakış | Görevler | Merge İstekleri | Branch'ler | İstatistikler
  | Deploy. Bu, GitHub'ın repo header'ındaki tab satırının karşılığı.
- Yeni bir `RepoTabBar` bileşeni (`components/RepoTabBar.tsx`), `AppLayout`
  içinde `useMatch` ile aynı şekilde tespit edilen `repo` değeri
  doğrultusunda `main`'in üstüne render edilir; route yapısı
  (`App.tsx`'teki path'ler) değişmez, sadece bu nav'ın nerede göründüğü
  değişir.

## 4. Görev takibi — kanban board

`RepoTasksPage.tsx`, düz `row-list`'ten 3 sütunlu bir board'a döner:

```
┌─ Yapılıyor ──────┐  ┌─ Test Bekliyor ──┐  ┌─ Bitti ──────────┐
│ [Acil] Görev1    │  │ Görev3           │  │ Görev5           │
│ Atanan: Ahmet    │  │ Atanan: Ayşe     │  │ Atanan: Mehmet   │
└──────────────────┘  └──────────────────┘  └──────────────────┘
```

- Sütunlar `TASK_STATUSES` sabitine (`labels.ts`) birebir karşılık gelir —
  yeni bir durum eklenmez.
- Kart sürükle-bırak ile sütun değiştirir; bırakma anında mevcut
  `api.updateTask(repo, task.id, { status })` çağrılır (aynı çağrı,
  `RepoTasksPage.tsx:59`'daki `patch` fonksiyonu zaten bunu yapıyor — yeni
  bir backend endpoint'i gerekmiyor). Sürükleme başarısız olursa (API
  hatası) kart eski sütununa geri döner ve `error` state'i bugünkü gibi
  gösterilir.
- Kart tıklanınca (sürükleme değil, click) bir detay paneli/modal açılır:
  açıklama, atanan kişi seçici, acil işaretleme, oluşturan/tarih — bugünkü
  satır-içi kontrollerin (`select`'ler, "Acil işaretle" butonu) taşındığı
  yer burası olur, board kartı sade kalır (başlık + acil rozeti + atanan
  kişi avatarı/baş harfleri).
- Görev oluşturma formu board'un altında bugünkü gibi kalır.
- Sürükle-bırak için ek bir kütüphane eklenmez — HTML5 native drag-and-drop
  (`draggable`, `onDragStart`/`onDrop`) yeterli, `frontend/package.json`'a
  yeni bağımlılık girmiyor.

## 5. Diğer liste sayfaları

Tüm Repolar, Denetim Kaydı, Bildirimler, Deploy Hedefleri sayfalarında
yapısal bir değişiklik yok — bu sayfalar zaten `row-list`/`card` deseniyle
tutarlı. Bu redesign'ın parçası olarak:

- Ortak `row-list` bileşenlerinin (badge yerleşimi, hover durumu, ikon
  hizalaması) tek bir tutarlı görünüme çekilmesi — mevcut CSS sınıfları
  korunur, sadece küçük tutarsızlıklar (varsa) giderilir.
- Bu sayfalarda yeni bir bileşen/route eklenmiyor.

## Etkilenen dosyalar (özet)

- `backend/internal/displaynames/` (yeni paket: store.go, store_test.go)
- `backend/internal/auth` veya `/api/me` handler'ı — `displayName` alanı eklenir
- `frontend/src/components/AppLayout.tsx` — sidebar sadeleştirme, brand adı
- `frontend/src/components/RepoTabBar.tsx` (yeni)
- `frontend/src/pages/RepoTasksPage.tsx` — kanban board'a dönüşüm
- `frontend/src/pages/AccessPage.tsx` — "Görünen adlar" bölümü
- `frontend/src/api/types.ts`, `client.ts` — `displayName` alanı, isim eşleme API'leri
