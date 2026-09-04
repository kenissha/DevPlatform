# DevPlatform — Nerede Kaldık

Son güncelleme: 2026-08-13 (gerçek SMTP gönderimi + gecelik repo yedeği + proje bazlı yetkilendirme eklendi)

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
`DEVPLATFORM_JWT_SECRET`. Gerçek e-posta göndermek için (varsayılan:
kapalı, sadece panel içi bildirim): `DEVPLATFORM_SMTP_HOST`,
`DEVPLATFORM_SMTP_PORT`, `DEVPLATFORM_SMTP_USERNAME`,
`DEVPLATFORM_SMTP_PASSWORD`, `DEVPLATFORM_SMTP_FROM`,
`DEVPLATFORM_BASE_URL`. Gecelik repo yedeği için (varsayılan: kapalı):
`DEVPLATFORM_BACKUP_DIR`, `DEVPLATFORM_BACKUP_HOUR` (varsayılan `2`).

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
"Kimlik doğrulama"). Lokalde (`npm run dev`) `/login` sayfasında
**"Yönetici olarak gir (yerel)"** butonu var (2026-09-04, sadece
`import.meta.env.DEV` iken render ediliyor, production build'e hiç
girmiyor) — tıklaman yeterli, elle JWT üretmene gerek yok:
`frontend/src/auth/devToken.ts`, Web Crypto API ile tarayıcıda gerçek
bir HS256 JWT imzalıyor (backend'in varsayılan
`DEVPLATFORM_JWT_SECRET`'ı olan `dev-not-a-real-secret` ile), admin rolüyle.

Elle token üretmek istersen (farklı bir subject/role denemek için):
HS256 ile, `DEVPLATFORM_JWT_SECRET` (varsayılan `dev-not-a-real-secret`)
kullanarak, şu claim'lerle: `sub`, `email`, `role` (`admin` | `developer`),
`exp`. İki şekilde kullanabilirsin:

- `http://localhost:5173/?token=<JWT>` adresine git (SSO devrini taklit eder), veya
- `/login` sayfasındaki "elle bir JWT ile" kutusuna yapıştır.

### Git ile kullanmak

Git artık kişi başına anahtarla çalışıyor — paylaşılan kullanıcı adı/şifre
yok. `main`'e doğrudan push geliştiriciler için **reddedilir** — "İnceleme
İsteği" açman gerekir. **Admin (sen) `main`'e doğrudan push atabilir**
(bkz. "Bilinmesi gereken kararlar"daki 2026-09-04 güncellemesi) — gerçek
merge/conflict çözümünü kendi bilgisayarında yapıp sonucu direkt push
etmen bunun için.

**Gerçek sunucuya karşı, önerilen yol (2026-09-03'ten beri):**
`devplatform-login` aracını kur (bkz. "CLI git login" güncellemesi,
aşağıda), sonra remote'u **token'sız** kullan:
```
irm https://<host>/api/devplatform-login/install.ps1 | iex
git clone https://<host>/git/<repo-adi>.git
```
İlk seferde Intranet kullanıcı adı/şifreni sorar, sonrasında hiç
sormaz. Sunucuda `DEVPLATFORM_LOGIN_CLI_PATH` ayarlı değilse bu iki
route 404 döner (bkz. `internal/logincli`).

**Lokal geliştirmede, ya da CLI aracı kurulu değilse — elle anahtar:**
Panelde "Hesabım" sayfasından (`POST /api/me/git-token`) kendi
anahtarını üret; ham anahtar sadece üretildiği an bir kere gösterilir, o an
kopyala — sonradan tekrar görüntülenemez.

```bash
git remote add origin http://<kendi-subject-iniz>:<git-token>@localhost:8080/git/<repo-adi>.git
git push origin <branch>
```

Kullanıcı adı, JWT'deki kendi `sub` claim'in; şifre ise üretilen git
anahtarı.

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
| Bildirim (panel içi + gerçek e-posta) | ✅ Bitti (2026-08-13) |

**Faz 1 tamamlandı** — e-posta gönderimi de dahil.

**2026-08-13 güncelleme — gerçek SMTP gönderimi:** `internal/notify.Store`
artık `net/smtp` tabanlı gerçek bir `SMTPEmailSender` ile bildirim
oluşturulduğunda e-posta da gönderebiliyor. Tasarım:

- Bağlama noktası tek yer: `Store.Create` içine `Sender`/`LookupEmail`/
  `BaseURL` opsiyonel alanları eklendi. `mergerequest`, `taskboard`,
  `deployment` — `Create`'i çağıran 3 paket — hiç değişmedi, otomatik
  olarak e-posta göndermeye başladılar.
- **Varsayılan olarak kapalı:** `DEVPLATFORM_SMTP_HOST` boşsa (varsayılan)
  davranış tamamen eskisi gibi — bildirimler sadece panelde kalır. Diğer
  `DEVPLATFORM_JWT_SECRET` gibi alanlarla aynı "güvenli varsayılan" deseni.
- Yeni ortam değişkenleri: `DEVPLATFORM_SMTP_HOST` (açma anahtarı),
  `DEVPLATFORM_SMTP_PORT` (varsayılan `25`), `DEVPLATFORM_SMTP_USERNAME` /
  `_PASSWORD` (boşsa AUTH denenmez), `DEVPLATFORM_SMTP_FROM` (varsayılan
  `devplatform@localhost`), `DEVPLATFORM_BASE_URL` (bildirim linkini
  e-postada tıklanabilir mutlak URL'e çeviriyor; boşsa link olduğu gibi
  kalır).
- STARTTLS sunucu destekliyorsa fırsatçı şekilde kullanılıyor (zorunlu
  değil — dahili röle sunucuları düz 25 portunda çalışabilir); AUTH sadece
  `Username` ayarlıysa VE sunucu destekliyorsa deneniyor; e-posta konusu
  (`Subject`) her zaman sabit bir metin — görev başlığı/branch adı gibi
  kullanıcı kontrollü içerik asla header'a girmiyor (header injection'a
  kapalı).
- `users.Store.Get(subject)` eklendi: bildirim alıcısının (JWT `sub`)
  e-posta adresini bulmak için kullanılıyor.
- Gerçek bir mail sunucusuna dokunmadan kanıtlandı: `internal/notify/smtp_test.go`
  ham TCP soketi üzerinde elle yazılmış sahte bir SMTP sunucusu çalıştırıyor
  (IIS için kullanılan sahte `CommandRunner` deseniyle aynı yaklaşım) — 
  STARTTLS farkındalığı ve koşullu AUTH gerçek protokol seviyesinde test
  edildi. Bilinçli olarak kapsam dışı bırakılan tek şey: TLS handshake'in
  kendisinin uçtan uca testi (zaten test edilmiş stdlib'e ince bir çağrı,
  emek/değer oranı düşük görüldü).

### Faz 2 — Otomasyon

| Parça | Durum |
|---|---|
| Build + versiyonlu klasör + IIS swap (temel mekanizma) | ✅ Bitti (2026-08-12), gerçek IIS'e karşı kanıtlandı |
| Secrets deposu (AES-256-GCM şifreleme + enjeksiyon) | ✅ Bitti (2026-08-12) |
| Onay akışına bağlama (panelden tetikleme) | ✅ Bitti (2026-08-13) |
| Gerçek Intranet-F/Intranet-B'ye bağlanma | ❌ Başlanmadı — bilinçli olarak sonraya bırakıldı |

**2026-08-13 güncelleme — panelden deploy:** `internal/deployment` artık
`internal/deploy`'un pipeline'ını gerçek bir onay akışına bağlıyor —
Geliştirici bir repo+ortam+branch için deploy isteği açar, Yönetici
panelden onaylar/reddeder, onay gerçekten checkout→build→versiyon→IIS
swap'ı çalıştırır ve sonucu (deploy edildi / başarısız + sebep) kaydeder.
Bununla birlikte:

- `deploy.Checkout` eklendi: bare bir repodan, go-git'in Clone'una
  gitmeden (Windows'ta yerel yol/URL ayrıştırma belirsizliği taşıyor),
  doğrudan tree'yi diske yazarak checkout yapıyor.
- Deploy hedefleri (hangi repo+ortam → hangi recipe/IIS sitesi) sabit,
  dosyadan yüklenen bir liste — `DEVPLATFORM_DEPLOY_TARGETS_FILE`. Boş/
  tanımsızsa hiçbir şey deploy edilemez (güvenli varsayılan); tasarım
  dokümanının "sabit listeden" şartı, panelden serbest metinle asla değil.
  **(Çözüldü — 2026-08-18: deploy hedefinin içeriği artık panelden
  yönetiliyor; sadece IIS site adları listesi hâlâ dosya tabanlı, bkz.
  "Bilinmesi gereken kararlar".)**
- **Bilinçli olarak yapılmadı:** bu oturumda gerçek hiçbir IIS sitesine
  (test ya da Intranet-F/B) dokunulmadı. Uçtan uca kanıt, gerçek IIS yerine
  sahte bir `CommandRunner`'a karşı yazılmış bir Go testinden geliyor
  (`TestApprove_RunsTheFullPipelineAgainstAFakeIIS` —
  `internal/deployment/handlers_test.go`): gerçek git checkout, gerçek npm
  build, gerçek versiyonlu klasör, sadece appcmd.exe çağrısı sahte.
  Gerçek bir siteyi hedef almak, kimsenin izlemediği bir oturumda değil,
  bilerek yapılacak sıradaki adım.

`internal/secretsvault` (`Store`, AES-256-GCM şifreleme) hazır ve test
edilmiş; `secretsTarget` alanı olan bir deploy hedefi onaylandığında
otomatik devreye giriyor (`DEVPLATFORM_SECRETS_KEY` ayarlıysa).

**Secrets deposu nasıl kullanılır:** Yönetici gerçek appsettings dosyasını
sunucuya elle koyar, `secretsctl -repo <ad> -environment <ad> -file <yol>`
çalıştırır (düz metni şifreleyip depoya yazar, kaynağı siler). Şifreleme
anahtarı `DEVPLATFORM_SECRETS_KEY` ortam değişkeninden gelir, diskte hiçbir
dosyada durmaz — bir şifre yöneticisinde saklanmalı, kaybedilirse şifreli
dosyalar kalıcı olarak açılamaz hale gelir. **Önemli:** `-environment`
değeri `Pipeline.Deploy`'a verilen `environment` ile birebir aynı yazılmalı
(büyük/küçük harf dahil) — `secretsctl` `production` ile kaydedip `Deploy`
`Production` ile ararsa dosya bulunamaz (hata verir, sessizce yanlış
davranmaz, ama karışıklığa yol açabilir).

**2026-08-12 güncelleme:** Son incelemede bulunan 2 önemli not kapatıldı —
`copyDir` artık recursive (alt klasörleri de kopyalıyor, `TestBuild_Npm_ProducesOutput`
nested dosya testiyle kanıtlı), ve `Pipeline.Deploy` artık `Prune` hatasını
gerçek deploy hatasından ayırt edebiliyor (`ErrPruneFailed`, `errors.Is` ile
yakalanabilir, `releaseDir` yine de dönüyor çünkü site gerçekten güncellendi).

Diğer küçük notlar (acil değil): `Deploy`'da henüz `context.Context` yok
(ileride iptal/timeout gerekecek), `appcmdPath()` 64-bit varsayıyor.

### Faz 3 — Genişleme

| Parça | Durum |
|---|---|
| İç git deposu yedeği (gecelik) | ✅ Bitti (2026-08-13) |
| Proje bazlı yetkilendirme | ✅ Bitti (2026-08-13) |
| Kişi ekleme/davet akışı | ✅ Bitti (2026-08-18 — DevPlatform'un kendi kodunda değil, Intranet-B/F'de) |

**2026-08-13 güncelleme — gecelik yedek:** `internal/backup` eklendi.
Sunucu her gün (varsayılan 02:00, `DEVPLATFORM_BACKUP_HOUR` ile
değiştirilebilir) tüm bare repoları `DEVPLATFORM_BACKUP_DIR` altına
kopyalıyor. Tasarım:

- **Varsayılan olarak kapalı:** `DEVPLATFORM_BACKUP_DIR` boşsa (varsayılan)
  hiçbir arka plan işi başlamıyor — diğer güvenli varsayılanlarla aynı desen.
- Kopyalama go-git'in Clone'undan geçmiyor (Windows yol/URL belirsizliği
  `deploy.Checkout`'ta olduğu gibi burada da sorun olurdu); doğrudan bare
  repo klasörünü dosya sistemi seviyesinde kopyalıyor.
- Her repo `<ad>.git.tmp` içine kopyalanıp sonra `<ad>.git`'e rename
  ediliyor — kopyalama yarıda kesilirse önceki gecenin iyi yedeği yerinde
  kalıyor, yarım/bozuk bir yedekle asla değişmiyor. Bir reponun kopyalanması
  başarısız olsa bile diğerleri etkilenmiyor (`Result.Errors`'ta raporlanıyor).
- `repostore.Store`'a `RootDir()` eklendi — `backup` paketinin bare repo
  klasörlerinin gerçek disk konumunu, `Store`'un kendi listeleme mantığını
  tekrarlamadan bulabilmesi için.
- **Henüz yapılmadı, bilinçli olarak:** yedek hedefi gerçek başka bir
  disk/makineye ("uzak" bir konuma) bağlanmadı — `DEVPLATFORM_BACKUP_DIR`'i
  gerçek bir hedefe (ayrı disk, ağ paylaşımı, başka makine) işaret etmek
  senin elinle yapılacak bir sonraki adım, tıpkı SMTP ve Intranet gibi.
- **Bilinen kapsam boşluğu (2026-09-03):** gecelik yedek sadece bare git
  repolarını kopyalıyor — `deploy-targets.json`, `access.json`,
  `display-names.json`, secrets vault dosyaları, `tasks/`,
  `merge-requests/`, `audit.jsonl` gibi diğer tüm `DataDir` içeriği hiç
  yedeklenmiyor. Sunucu diski kaybedilirse repo'lar güvende olur (GitHub'a
  da push'lanıyorsa) ama bu ayarların hepsi kaybolur. Gelecekte
  `backup.Run`'ı (ya da yanına yeni bir fonksiyonu) tüm `DataDir`'i
  kapsayacak şekilde genişletmek — henüz yapılmadı, bilinçli olarak
  sonraya bırakıldı.

**2026-08-13 güncelleme — proje bazlı yetkilendirme:** `internal/access`
eklendi. Yönetici artık belirli bir kişiyi, sadece izin verilen repolarla
sınırlayabiliyor — panelde yeni "Proje erişimi" sayfası (sadece yönetici
görür).

- **Varsayılan: kısıtlanmamış.** Erişim kaydı hiç ayarlanmamış biri tüm
  repoları görür — bugünkü davranışın aynısı. Bu, deploy hedefleri
  listesinin tersi bir tercih: deploy hedefleri boşken "hiçbir şey deploy
  edilemez" güvenli varsayılandı çünkü deploy zaten yeni bir yetenekti; repo
  görünürlüğü ise herkesin zaten sahip olduğu bir yetenek olduğu için,
  "varsayılan olarak kısıtlı" burada bu özelliğin devreye girdiği anda
  mevcut kullanıcıları kilitler, hiç kimseye bir şey geri verilmeden önce.
  Yönetici belirli bir kişiyi bilerek kısıtlamaya dahil ediyor; bu paketin
  var olması kimseyi otomatik olarak kısıtlamıyor.
- **İki seviyeli uygulama:** `access.RequireRepoAccess` middleware'i
  `server.go`'daki her `{repo}` içeren rotayı (branch'ler, görevler, merge
  istekleri, istatistikler, deploy istekleri) korurken; `/api/repos`,
  `/api/tasks`, `/api/merge-requests`, `/api/deployments` gibi "tüm
  repolar" görünümleri kendi sonuçlarını ayrıca süzüyor (`Access` alanı
  üzerinden) — yoksa kısıtlı biri, doğrudan erişemediği bir reponun
  öğelerini "tüm repolar" görünümünden yine de görebilirdi.
- **Yönetici her zaman istisna.** `auth.RequireRole`'ün "RoleAdmin her
  kontrolü geçer" kuralıyla aynı: bir yöneticiyi kısıtlamak, kısıtlamayı
  yönetmeyi imkansız hale getirirdi.
- Yeni admin-only API: `GET/PUT/DELETE /api/access(/{subject})`.
- Gerçek çalışan sunucuya karşı elle doğrulandı (sadece testlerle değil):
  iki repo oluşturuldu, bir geliştirici birine kısıtlandı, her repo-scoped
  rotanın izin verilmeyen repo için 403 döndüğü, `/api/repos`'un süzüldüğü,
  yöneticinin etkilenmediği ve kısıtlama kaldırılınca her iki reponun da
  geri geldiği doğrulandı.
- **Görsel/tarayıcı testi yapılmadı** — bu ortamda tarayıcı otomasyon aracı
  yok. Frontend `tsc -b && vite build` ve `oxlint` ile temiz derleniyor,
  API sözleşmesi gerçek sunucuya karşı curl ile doğrulandı, ama "Proje
  erişimi" sayfasının kendisi bir tarayıcıda tıklanarak denenmedi.

**2026-08-13 güncelleme — güvenlik incelemesi (ev oturumunun 9 commit'i):**
Yukarıdaki dört parça (panelden deploy onayı, gerçek SMTP, gecelik yedek,
proje bazlı yetkilendirme) bir ev oturumunda yazılmıştı ve bu projenin
normal task-review sürecinden hiç geçmemişti. Dört ayrı, kapsamlı review
dispatch edildi (her biri kendi diff'i + proje güvenlik kısıtlarıyla);
bulunan ve düzeltilen gerçek hatalar:

- **Kritik — eşzamanlı onay yarışı:** `deployment.Approve`'da kilit yoktu;
  iki eşzamanlı onay isteği aynı IIS site'ına yarışarak deploy
  edebiliyordu. `Store.Claim` eklendi (pending→in-progress, mutex'li),
  ikinci onay artık anında reddediliyor.
- **Kritik — checkout klasörü hiç oluşturulmuyordu:** temiz bir kurulumda
  ilk gerçek onay `ENOENT` ile 500 dönerdi (fake `t.TempDir()` testlerde
  görünmüyordu çünkü klasör zaten testte hazır oluşturuluyordu). `main.go`
  artık `CheckoutRoot`'u da `MkdirAll` ediyor.
- Deploy hatası artık panelde ham haliyle görünmüyor (build çıktısı,
  secrets, mutlak yollar içerebiliyordu) — sınıflandırılmış kısa mesaja
  indirgendi, ham hata sadece sunucu logunda.
- `DEVPLATFORM_DEPLOY_TARGETS_FILE` artık başlangıçta doğrulanıyor:
  aynı (repo, environment) çiftinin tekrarı, bilinmeyen recipe, boş
  siteName, path-traversal'lı secretsTarget artık sunucuyu başlatmıyor
  (sessizce yanlış konfigürasyonla ayağa kalkmak yerine).
  **(Çözüldü — 2026-08-18: bu doğrulama artık başlangıçta değil, panelden
  bir hedef kaydedilirken yapılıyor — `TargetStore.Set`, bkz. "Bilinmesi
  gereken kararlar" bölümündeki 2026-08-18 güncellemesi.)**
- Deploy artık süresiz asılı kalamıyor — zaman aşımı eklendi, aşılırsa
  istek "failed" olarak işaretleniyor.
- Gecelik yedekte finalize sırası değişti (önce eskiyi kenara al, yeniyi
  yerine koy, en son eskiyi sil) — arada çökme olursa artık her ihtimalde
  ya eski ya yeni yedek diskte duruyor, hiçbir zaman ikisi de yok değil.
- `DEVPLATFORM_BACKUP_DIR`, repo deposunun kök diziniyle çakışırsa artık
  yedek devre dışı kalıyor (yanlış konfigürasyonla canlı repoların
  silinmesini önlemek için).
- `/api/audit` artık `access.Store`'a göre süzülüyor — kısıtlı bir
  geliştirici artık erişemediği bir reponun audit kayıtlarını göremiyor.
- `DELETE /api/access/{subject}` için router seviyesinde admin-only testi
  eklendi (GET/PUT zaten test ediliyordu, DELETE eksikti).
- SMTP tarafında, CRLF içeren bir alıcı adresinin header injection'a yol
  açamayacağını kanıtlayan bir regresyon testi eklendi (koruma zaten
  `net/smtp`'nin kendisinden geliyordu, ama bu projenin kendi testi yoktu).

Dördü de `go build`, `go vet`, `go test ./...` (21 paket) ve frontend
`tsc -b && vite build` ile temiz. Commit'ler:
`9f840a2` (deploy), `53500a9` (backup), `9cdfb90` (access/audit),
`ab9273b` (SMTP).

**Çözülmeden bırakılan, gerçek bir mimari karar gerektiren iki bulgu**
(aşağıdaki "Bilinmesi gereken kararlar" bölümüne taşındı):
1. Git push/pull hâlâ tek paylaşılan `DEVPLATFORM_GIT_USERNAME/PASSWORD`
   kullanıyor — proje bazlı yetkilendirme sadece panelde geçerli, git
   katmanında geçerli değil.
2. Deploy pipeline, repo'nun kendi build script'lerini appcmd'nin
   çalışması için Administrator yetkili bir hesapla çalıştırıyor.

**2026-08-13 güncelleme — IIS yardımcı servisi (yetki ayrımı):**
`internal/iishelper` ve `cmd/iishelper` eklendi. `devplatform.exe` artık
appcmd.exe'yi hiç doğrudan çalıştırmıyor — appcmd'yi çalıştıran tek şey,
ayrı, küçük bir Windows Service (`iishelper`), `LocalSystem` hesabıyla
çalışıyor ve yerel bir named pipe üzerinden sadece tek bir işlemi kabul
ediyor: bilinen bir IIS site'ının fiziksel yolunu mutlak bir dizine
çevirmek. Gelen her istek bu tek şekle tam uymuyorsa reddediliyor —
çağıranın (`devplatform.exe`) gönderdiği appcmd yoluna güvenilmiyor,
servis kendi yolunu bağımsızca hesaplayıp karşılaştırıyor.

Sonuç: `devplatform.exe` artık hiçbir zaman Administrator yetkisiyle
çalışmasına gerek yok — repo'nun kendi build script'i (`npm run build`/
`dotnet publish`) her zaman olduğu gibi çalışıyor ama artık Admin
yetkisiyle değil. Sadece `iishelper` servisi (dar, sabit, tek işlemli)
yükseltilmiş yetkiyle çalışıyor.

Kurulum: `backend/cmd/iishelper/install.ps1`, bir kere elevated
PowerShell'den çalıştırılır, servisi kaydeder ve named pipe'ı
`devplatform.exe`'nin çalıştığı hesaba kısıtlayacak `DEVPLATFORM_IISHELPER_SDDL`
değerini üretip ekrana yazar — bu değeri servisin ortam değişkenlerine
elle eklemek gerekiyor (diğer `DEVPLATFORM_*` gizli değerleri gibi).

**Henüz yapılmadı:** gerçek servis kurulup, mevcut test IIS site'ına
karşı uçtan uca canlı doğrulama (bkz. plan dosyasının sonundaki
"gözetimli doğrulama" adımları) — bu, orijinal IIS kanıtlamasında
yapıldığı gibi birlikte, elle yapılacak.

**2026-08-18 güncelleme — kişi ekleme/davet akışı (Faz 3'ün son parçası):**
Bu iş DevPlatform'un kendi kodunda değil, **Intranet-B/F'de** çözüldü —
DevPlatform zaten SSO üzerinden Intranet'e güveniyor, dolayısıyla "kişi
ekleme" zaten Intranet-B'nin `DevPlatformYetkiController`'ı üzerinden
(admin panelden rol atama) çalışıyordu; eksik olan tek şey, rol
verildiğinde kişiye haber veren bir şeydi.

- Intranet-B: `SetKullaniciYetkisi`, birine **ilk kez** DevPlatform rolü
  verildiğinde (rol değişiminde veya kaldırmada değil), mevcut
  `IEmailQueue`/`EmailService` altyapısı üzerinden kurumsal imzalı
  şablonla bir davet maili kuyruğa ekliyor.
- Intranet-F: yeni `/devplatform-giris` rotası (`PrivateRoute` arkasında)
  — mail linkine tıklandığında kişi zaten Intranet'e girişliyse anında
  DevPlatform'a yönlendiriliyor; girişli değilse normal AD login
  ekranını görüp giriş yapınca otomatik olarak buraya (ve oradan
  DevPlatform'a) dönüyor. **Bilinçli tasarım kararı:** mail'in içine
  hazır bir DevPlatform token'ı gömülmüyor — güven her zaman o anki
  Intranet oturumundan geliyor, mail sadece oraya bir kısayol.
- DevPlatform tarafında değişen hiçbir şey yok; bu not sadece Faz 3'ün
  tamamlandığını kayda geçirmek için burada.

## Sıradaki iş

**2026-08-14 — gerçek sunucuya ilk kurulum yapıldı.** `devplatform.exe`
artık gerçek (başka canlı projeleri de barındıran) sunucuda, IIS
`httpPlatformHandler` ile çalışıyor durumda; panelde Yönetici olarak
giriş doğrulandı. Henüz yapılmayanlar: git kısmı (repo yok), gerçek
deploy hedefleri, dışarıdan (sunucunun dışından) erişim, ve hâlâ açık
duran git kimlik doğrulama kararı (bkz. "Bilinmesi gereken kararlar").

Faz 1 bitti (SMTP dahil). Faz 2'nin build+deploy+rollback mekanizması
artık panelden gerçekten tetiklenebiliyor (bkz. yukarısı). Faz 3
tamamen bitti: gecelik yedek, proje bazlı yetkilendirme, git seviyesinde
kişi başına erişim (bkz. "Bilinmesi gereken kararlar") ve kişi
ekleme/davet akışı (bkz. yukarıdaki 2026-08-18 güncellemesi — bu son
parça DevPlatform'un kendi kodunda değil, Intranet-B/F'de çözüldü).
Kalan gerçek iş artık tamamen kod değil, **ops + gözetimli bir oturum**
gerektiriyor: sunucuda `DEVPLATFORM_ALLOWED_SITES_FILE`'ı gerçek IIS site
adlarıyla oluşturmak (bkz. "Bilinmesi gereken kararlar" bölümündeki
2026-08-18 güncellemesi), sonra gerçek Intranet-F/Intranet-B hedeflerini panelden
("Deploy Hedefleri" sayfası) eklemek ve ilk gerçek deploy'u birlikte
izlemek (recipe'ler, gerçek appsettings için secretsctl ile secrets'ı
önceden yüklemek gerekecek — "IIS / deploy — canlıda öğrenilen dersler"
bölümündeki 3 nottan özellikle üçüncüsüne, içerik konumuna, dikkat).
Gerçek SMTP sunucu bilgilerini (`DEVPLATFORM_SMTP_*`) ve gerçek yedek
hedefini (`DEVPLATFORM_BACKUP_DIR`) girmek de aynı şekilde senin elinle,
gözetimli yapılacak birer adım.

## Bilinmesi gereken kararlar

- **2026-09-04 güncelleme — açık/koyu tema ve yeni Panel:**
  Açık tema paleti `index.css`'te zaten tanımlıydı ama sadece işletim
  sistemini takip ediyordu. Artık üst barda güneş/ay butonu var
  (`frontend/src/theme/`): seçim `localStorage`'a yazılıp `<html>`
  üzerine `data-theme` olarak basılıyor. Üç durum var, iki değil —
  hiç seçim yapılmadığında **hiçbir şey basılmıyor**, böylece CSS'in
  `prefers-color-scheme` sorgusu devreye giriyor ve OS takip ediliyor.
  Media query bu yüzden `:root:not([data-theme="dark"])` ile korunuyor:
  aksi halde açık OS'ta koyuyu seçen biri yine açık tema görürdü.
  Panel sayfası (`DashboardPage.tsx`) baştan yazıldı: selamlama +
  "bekleyen işin" özeti, üç odak kartı (bana atanan / inceleme bekleyen
  / onay bekleyen deploy — her biri ilk 3 gerçek işi de gösteriyor),
  `/api/audit`'ten beslenen "Son hareketler" akışı, ve sağ rayda ekip
  iş yükü + repo listesi. Yeni backend toplaması yok; tek eklenen şey
  `/api/users`'ın artık `displayName` de döndürmesi
  (`/api/display-names` sadece admin'e açık olduğu için geliştiriciler
  ham subject id'si — "7 açtı" — görüyordu; bu endpoint zaten herkesin
  e-postasını veriyor, isim ondan daha hassas değil).

  Panele ayrıca **katkı ısı haritası** eklendi (GitHub'daki yeşil
  kareler): `GET /api/contributions?days=365` →
  `gitstats.ActivityByAuthor`, erişilebilen tüm repoları gezip **çağıran
  kişinin kendi** commit'lerini gün gün sayıyor. Endpoint'te bilerek
  `?subject=` yok — her zaman çağıranı cevaplıyor, URL'i kurcalayarak
  başkasının aktivitesine dönüştürülemiyor.
  Commit'ler **git yazar e-postasıyla** eşleştiriliyor (büyük/küçük
  harf duyarsız) — bir commit nesnesinde başka kimlik yok.
- **2026-09-04 güncelleme — "Git e-postaların" (`internal/gitemails`):**
  Yukarıdaki eşleştirmenin gerçek hayattaki sorunu: git commit'e,
  o makinedeki `git config user.email` ne yazıyorsa onu damgalıyor ve
  bu genellikle panel hesabının (SSO'dan gelen) e-postası **değil**.
  Örnek: bu projenin sahibinin commit'leri `rifatozturk061@gmail.com`,
  panel hesabı ise `rifat.ozturk@sigortatahkim.org` — yani grafik
  hiçbir şey göstermeyecekti. GitHub'ın "hesabına birden fazla e-posta
  tanımla" çözümünün aynısı eklendi: Hesabım sayfasında kişi kendi
  commit adreslerini ekliyor, `Contributions` panel e-postası **artı**
  bu listeyle eşleştiriyor (`gitstats.ActivityByAuthors`).
  Uçlar: `GET/POST /api/me/git-emails`, `DELETE /api/me/git-emails?email=`
  — üçü de sadece çağıranın kendi listesini görüyor/değiştiriyor, URL'de
  subject taşınmıyor, bu yüzden admin yetkisi de gerekmiyor (kişisel
  ayar, yönetimsel değil).
  **Doğrulama gerekmiyor, bilinçli:** buraya yazılan adres kimseye
  erişim vermiyor ve yeni bir bilgi de açmıyor — kimin ne zaman commit
  attığı zaten katkıda bulunanlar ve denetim kaydı üzerinden her
  kullanıcıya görünür. Sadece kişinin **kendi** grafiğinde hangi
  commit'lerin sayılacağını genişletiyor. SSO arkasındaki 2 kişilik bir
  ekip için e-posta doğrulama turu gereksiz karmaşıklık.
- **2026-09-04 güncelleme — branch'ten inceleme isteği açma (GitHub
  mantığında):** Eski akış "İnceleme İstekleri" sayfasında kaynak/hedef
  branch seçen bir formdu. Artık `/repos/:repo/branches` sayfasındaki
  her branch tıklanabilir — kendi sayfasına götürüyor
  (`/repos/:repo/branches/*`, splat route çünkü branch adları slash
  içerebiliyor, örn. `feature/hakem-raporlari`). O sayfada: main'e göre
  bu branch'in eklediği commit'ler (yeni: `gitstats.CommitsAhead`,
  `GET /api/repos/{repo}/branches/{branch}/commits`), değişiklik özeti
  (yeni: `mergerequest.Handlers.BranchPreview`,
  `GET /api/repos/{repo}/branches/{branch}/preview` — mevcut `Diff`'i
  tekrar kullanıyor), ve tek butonla **"İşim bitti, incele"**. Bu buton
  zaten açık bir istek varsa onu gösteriyor, en son reddedilmişse notunu
  gösterip "Tekrar iste" sunuyor. "İnceleme İstekleri" sayfası artık
  sadece salt-okunur geçmiş — yeni istek açma formu tamamen kaldırıldı.
- **Çözüldü — git artık kişi başına anahtarla çalışıyor (2026-08-17):**
  `internal/gitauth`'ın tek paylaşılan `DEVPLATFORM_GIT_USERNAME`/
  `_PASSWORD` çifti tamamen kaldırıldı (geçiş dönemi yok). Yeni
  `internal/gittoken`, kişi başına tek bir anahtarın SHA-256 hash'ini
  saklıyor; ham anahtar hiç diskte durmuyor, sadece üretildiği an bir
  kere gösteriliyor (panelde "Hesabım" sayfası, `POST /api/me/git-token`).
  `/git/` rotasının önündeki `RequireTokenAndAccess` ara katmanı, panelin
  zaten kullandığı `access.Store.CanAccess`'in **aynısını** çağırıyor —
  git için ayrı bir yetki sistemi yok. Ayrıntı için
  `docs/superpowers/specs/2026-08-17-per-user-git-access-design.md`.
  Paylaşılan şifreyle git kullanan biri varsa (şimdiye kadar sadece biz),
  bu değişiklikten sonra Hesabım sayfasından yeni bir anahtar üretmesi
  gerekiyor — eski paylaşılan şifre artık hiçbir yerde geçerli değil.
- **Bilinçli karar — git kimlik bilgileri açık HTTP üzerinden düz metin
  gidiyor:** Panel ve `/git/` şu an düz HTTP üzerinden sunulduğu için
  (yukarıya bakın), git Basic Auth kimlik bilgileri ve anahtar üretme
  cevabındaki ham anahtar ağda düz metin olarak taşınıyor. Bu, kişi
  başına anahtar geçişiyle gelen yeni bir zafiyet **değil** — eski
  paylaşılan şifre de aynı şekilde açıktı. Ama kişiye özel bir anahtar,
  herkesin zaten bildiği tek bir paylaşılan şifreden bir saldırgan için
  daha değerli, bu yüzden bu maruziyetin örtük bir varsayım değil, kayıtlı
  ve bilinçli bir karar olması gerekiyor.
- **Çözüldü — build adımı artık Administrator yetkisiyle çalışmıyor
  (2026-08-13):** bkz. yukarıdaki "IIS yardımcı servisi" güncellemesi.
  `deploy.Pipeline`'ın build adımı hâlâ `devplatform.exe` içinde
  çalışıyor ama artık asla yükseltilmiş yetkiyle değil; appcmd'yi
  çalıştıran tek şey ayrı, dar yetkili `iishelper` servisi. Kalan tek
  gerçek iş: gerçek sunucuda servisi kurup canlı doğrulamak (kod değil,
  ops adımı).

- **Proje erişimi varsayılan olarak kısıtlanmamış, ayrıntı için yukarıdaki
  2026-08-13 güncellemesine bakın.** Kısa özet: `internal/access`'te
  erişim kaydı olmayan biri her repoyu görür. Bunu tersine çevirip
  "varsayılan kısıtlı" yapmak, mevcut kullanıcıları (senin ve iş
  arkadaşının hesapları dahil) kimseye hiçbir şey atanmadan kilitler —
  yapılırsa önce her ikisine de tüm repolar elle atanmalı.
- **Kimlik doğrulama AD'ye doğrudan bağlanmıyor.** Gerçek giriş, senin
  mevcut sisteminde yapılıyor; buraya imzalı bir JWT devrediliyor.
  Değiştirilmesi gereken tek yer `backend/internal/auth/auth.go` —
  dosyanın başındaki yorumda tam olarak neyin ayarlanacağı yazıyor
  (algoritma HS256 varsayılıyor, rol `role` claim'inden okunuyor).
- **`main`'e ilk commit merge isteğiyle girer.** Yeni repo boşken `main`
  ref'i yoktur ve doğrudan push kapalıdır; hedefi `main` olan bir merge
  isteği onaylandığında branch o an oluşturulur.
- **Çözüldü — "Merge İsteği" artık "İnceleme İsteği", gerçek merge
  platformdan çıktı (2026-09-04):** Eski model (fast-forward-only,
  go-git alfa sürümünde gerçek 3-way merge yoktu, branch'ler ayrışırsa
  409 ile reddedip açık bırakıyordu) tamamen kaldırıldı — bu artık bir
  eksiklik değil, bilinçli bir mimari. `mergerequest.Handlers.Approve`
  hiçbir git işlemi yapmıyor, sadece durum+not kaydı. `main`'i gerçekten
  ilerleten tek yol artık bir **Admin'in kendi doğrudan push'u**:
  `gitserver`'ın korumalı-ref engeli artık kimin push attığını biliyor
  (`gittoken.RequireTokenAndAccess` zaten admin'i tespit ediyordu, bu
  bilgiyi `gitserver.WithAdmin`/`IsAdmin` ile context üzerinden
  taşıyoruz) — geliştiriciler hâlâ `main`'e doğrudan push atamaz, Admin
  atabilir. Akış: geliştirici branch açar, işi bitince "İnceleme İsteği"
  açar; Admin (gerekirse Claude ile) gerçek git kullanarak (bu projenin
  pinlenmiş go-git sürümüyle sınırlı olmadan) kendi bilgisayarında
  merge/conflict çözümü yapar, `main`'i doğrudan push eder, sonra
  panelden "Onayla" ile sadece "bunu ben yaptım" kaydı bırakır. Reddet
  artık kısa bir not taşıyabiliyor ("şunu düzelt, tekrar aç") — yeniden
  açma mekanizması yok, geliştirici aynı branch'te devam edip yeni bir
  istek açıyor. `mergerequest/merge.go` (FastForwardMerge) tamamen
  silindi.
- **Audit log yalnızca eklenir.** Dosya hiçbir zaman yeniden yazılmaz.
  Dosya sistemine erişimi olan biri yine de düzenleyebilir; gerçek
  kurcalama kanıtı (hash zinciri / dış sunucu) kapsam dışı bırakıldı.
- **Veri tabanı yok.** Her şey `DEVPLATFORM_DATA_DIR` altında düz dosya:
  bare git repoları, `tasks/`, `merge-requests/`, `users.json`,
  `audit.jsonl`. 2 kişilik ekip için kasıtlı bir tercih; büyürse
  taşınması gereken ilk yer burası.
- **Frontend artık `devplatform.exe`'nin kendisinden servis edilebiliyor
  (2026-08-14).** Gerçek `go:embed` değil ama aynı sonucu veriyor:
  `DEVPLATFORM_FRONTEND_DIR` build edilmiş `frontend/dist`'e işaret
  ederse, backend onu SPA-fallback'li şekilde aynı origin'den sunuyor
  (`cmd/devplatform/frontend.go`). Boşsa (yerel geliştirmede olduğu
  gibi) hiçbir şey değişmiyor. Bunun sebebi kozmetik değildi: frontend
  kodu CORS/ayrı API adresi hiç desteklemiyor, üretimde ikisi aynı
  origin'den gelmek zorunda.
- **Çözüldü — deploy hedefleri artık panelden yönetiliyor (2026-08-18):**
  Eski tek dosyanın iki işi ayrıldı. Hedefin içeriği (repo, environment,
  recipe, siteName, secretsTarget, keepVersions) artık
  `internal/deployment.TargetStore` diye panelden CRUD edilen bir depoda
  (`DataDir/deploy-targets.json`) — yeni "Deploy Hedefleri" admin
  sayfası, `GET/PUT/DELETE /api/deploy-targets(/{repo}/{environment})`.
  Hangi IIS site adlarına dokunulabileceği ise hâlâ sadece sunucuya elle
  yazılan, küçük, ayrı bir dosyada (`DEVPLATFORM_ALLOWED_SITES_FILE`,
  eski `DEVPLATFORM_DEPLOY_TARGETS_FILE`'ın yerine geçti) — panel bu
  listeye asla yazamaz, sadece `GET /api/allowed-sites` ile okuyup bir
  dropdown'da gösterir. Bu ayrım kasıtlı: `iishelper`'ın var oluş
  sebebini (devplatform.exe ele geçirilse bile appcmd'nin sadece
  önceden onaylı site'lara dokunabilmesi) koruyor. Ayrıntı için
  `docs/superpowers/specs/2026-08-18-deploy-target-management-design.md`.
  Site listesi değiştiğinde (yeni bir IIS site'ı elle açıldığında) hem
  `iishelper` hem `devplatform.exe` yeniden başlatılmalı — ikisi de bu
  dosyayı sadece süreç başlarken bir kere okuyor.

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

## IIS `httpPlatformHandler` ile gerçek sunucuya kurulum — öğrenilen dersler (2026-08-14)

DevPlatform ilk kez gerçek (canlı başka projeleri de barındıran) sunucuya
kuruldu — kod değişikliği gerekmedi, tamamen IIS/işletim sistemi tarafında
4 gerçek engel çıktı. Kullanılan desen: `OasRapor\go backend\web.config`'te
zaten kanıtlanmış olan desenin aynısı — backend `httpPlatformHandler` ile
IIS'e bir "site" olarak tanıtılıyor (sabit bir portta dinliyor, örn.
`:8081`), frontend ayrı bir statik IIS site'ı olarak duruyor ve kendi
`web.config`'indeki `<rewrite>` kurallarıyla `/api/*` ve `/healthz`'i
`127.0.0.1:8081`'e yönlendiriyor (reverse proxy). Tarayıcı tek bir origin
görüyor, CORS sorunu hiç çıkmıyor.

- **`httpPlatformHandler`, `stdoutLogFile`'ın klasörünü kendisi
  oluşturmuyor.** `web.config`'te `stdoutLogFile="...\logs\stdout"`
  yazsan bile `logs` klasörü elle, önceden oluşturulmuş olmalı — yoksa
  process başlatılamıyor (log da hiç yazılmıyor, sessizce 502 dönüyor).
- **Reverse-proxy `<rewrite>` kuralları, sunucu genelinde "Enable
  proxy" açık olmadan çalışmaz.** IIS Manager'da sunucu düğümü →
  Application Request Routing Cache → Server Proxy Settings → "Enable
  proxy" işaretli olmalı. Bu kapalıyken hedef adres (`127.0.0.1:8081`)
  kendisi tamamen sağlıklı olsa bile (`/healthz` doğrudan çalışıyor
  olsa bile) rewrite üzerinden gelen her istek 502 ile geri dönüyor —
  yanıltıcı çünkü "backend çalışmıyor" izlenimi veriyor, aslında
  backend'le hiç ilgisi yok.
- **`httpPlatformHandler`'la başlatılan backend'in kendi IIS site
  binding'i (`127.0.0.1:8082` gibi) gerçek trafik için önemli değil.**
  O binding sadece IIS'in process'i "bir site" olarak tanıyıp
  başlatabilmesi için var; gerçek trafik doğrudan `DEVPLATFORM_LISTEN_ADDR`
  ile backend'in kendi dinlediği sabit porta gidiyor (frontend'in
  `web.config`'i o adrese yönlendiriyor). Bu port'a bağlanmaya çalışmak
  yanlış teşhise götürebilir — asıl test edilmesi gereken, backend'in
  kendi gerçek portu.
- **Aynı porta birden fazla site bağlı olabilir, IIS bunu sessizce
  çözüyor.** Yeni bir site için port seçerken, o portun listede
  başka bir site tarafından da kullanılmadığından emin olunmalı — yoksa
  hangi site'ın cevap vereceği belirsiz/yanıltıcı olabiliyor (bizde,
  yanlış site'a tıklanmış olması asıl sorunla karışıp vakit kaybettirdi).
- **En sık karışıklık kaynağı, kod değil insan hatasıydı:** frontend'in
  fiziksel klasörüne yanlışlıkla başka bir projenin build çıktısı
  kopyalanmıştı. IIS Manager'da bir site'ın **Explore** (fiziksel
  klasörü aç) seçeneğiyle "gerçekten hangi dosyalar orada" diye
  doğrulamak, port/binding'le uğraşmadan önce ilk kontrol edilecek şey
  olmalı.

**Sonuç: backend artık `httpPlatformHandler` ile barındırılmıyor
(2026-08-14).** Yukarıdaki ayarları (`AlwaysRunning`, `Idle Time-out=0`,
`Preload Enabled`) doğru yapsak bile süreç durmaya devam etti; asıl
mesele ayar değil, **model uyumsuzluğuydu**: `httpPlatformHandler`
"istek gelince çalış, gelmeyince uyu" mantığında, ama bu süreç istek
olmadan da iş yapmak zorunda — gecelik yedek zamanlayıcıyla çalışıyor,
onaylanan bir deploy ise kendisini tetikleyen HTTP isteği çoktan
cevaplandıktan sonra dakikalarca sürüyor. İkisi de sessizce atlanır ya
da yarıda kesilirdi. Bu yüzden `devplatform.exe` artık **Windows
Servisi** olarak çalışıyor (`cmd/devplatform/install.ps1`), tıpkı
`iishelper` gibi. IIS'in rolü değişmedi: frontend site'ı hâlâ `/api`,
`/healthz` ve `/git`'i servisin portuna reverse-proxy'liyor.

**Genel ders:** IIS'in `httpPlatformHandler`'ı, arka planda zamanlanmış
işi ya da isteğin ömrünü aşan işi olan bir süreç için yanlış barındırma
modeli. Böyle bir süreç Windows Servisi olmalı; IIS sadece önüne
proxy olarak konulmalı.

**2026-09-03 güncelleme — CLI git login (`devplatform-login`):**
Token bazlı git kimlik doğrulaması, tek-kişi-tek-token modelinden
(yeni token üretmek eskisini sessizce geçersiz kılıyordu) çoklu-token,
bağımsız iptal edilebilir modele geçti (`internal/gittoken`, panelde
"Hesabım" sayfası). Ayrıca yeni bir Windows CLI aracı eklendi:
`devplatform-login`, git'in credential-helper protokolünü uyguluyor —
kurulduktan sonra `git clone`/`pull`/`push` token'sız remote URL'lerle
çalışıyor, ilk seferde Intranet kullanıcı adı/şifresini soruyor,
DPAPI ile şifrelenmiş yerel önbellekte tutuyor, token iptal edilirse
bir sonraki git işleminde otomatik tekrar soruyor.

**Gözetimli doğrulama tamamlandı (2026-09-03), gerçek terminalde, gerçek
AD hesabıyla, `rifat.ozturk`'ün kendi makinesinde:**
1. ✅ `devplatform-login.exe install` git config'i doğru ayarladı VE git
   helper'ı fiilen çağırdı (`oasrapor-test` reposu ile).
2. ✅ Önbellekte kayıt yokken taze bir `git clone` gerçek konsolda
   kullanıcı adı/şifre sordu ve doğru girişten sonra başarıyla
   tamamlandı.
3. ✅ Hemen ardından bir `git pull` hiç sormadı (önbellek isabeti).
4. ✅ Panelden token iptal edilince, aynı komut içinde değil ama **bir
   sonraki ayrı git işleminde** otomatik olarak (`erase` üzerinden)
   tekrar sordu — opak bir 401 ile değil.
5. ✅ Yanlış şifre net bir Türkçe hata gösterdi
   (`devplatform-login: giriş başarısız: intranet girişi 401 döndü —
   kullanıcı adı/şifre hatalı olabilir`). Bunun ardından git'in **kendi
   yerleşik** (bizim aracımızla ilgisi olmayan) "Username for..."
   sorusu devreye girdi — bu, helper credential döndürmeden çıkınca
   git'in normal yedek davranışı; o soru artık AD şifresini değil,
   doğrudan git anahtarını (subject + git-token) istiyor, karıştırılmaya
   açık ama beklenen bir davranış.

**2026-09-03 güncelleme — tek satırlık kurulum (`internal/logincli`):**
Yanlış şifrede artık `promptAndLogin` aynı kullanıcı adıyla **1 kez
daha** deniyor (`ErrBadCredentials` sentinel'i üzerinden ayırt
ediliyor) — en yaygın durum (yazım hatası) için git'in kendi yedek
promptuna hiç düşülmüyor.

Ayrıca exe'nin makineden makineye elle taşınması yerine tek satırlık
kurulum eklendi: sunucu artık `devplatform-login.exe`'yi ve bir
kurulum script'ini kendi servis ediyor (auth gerektirmiyor — exe'de
sır yok, indirmek erişim vermiyor):
```
irm https://<host>/api/devplatform-login/install.ps1 | iex
```
Sunucuda ayarlanması gereken tek şey: `DEVPLATFORM_LOGIN_CLI_PATH`
ortam değişkenini, build edilmiş `devplatform-login.exe`'nin gerçek
disk yoluna işaret edecek şekilde ayarlamak. Boşsa (varsayılan) her
iki route da 404 döner — diğer her şeyde kullanılan "bilinçli
yapılandırılana kadar hiçbir şey" deseniyle aynı. `devplatform.exe`'yi
güncellerken bu exe'yi de aynı elle-kopyalama adımına dahil et (build
alırken AV yanlış pozitifini önlemek için yukarıdaki
`-trimpath -ldflags="-s -w"` bayraklarını unutma).

**Canlı test sırasında bulunup düzeltilen 4 gerçek ops sorunu:**
- Sunucudaki eski (tek-token) `git-tokens.json` dosyası yeni çoklu-token
  formatına otomatik yükseltilmiyordu — ilk deploy'da hem Hesabım
  sayfası 500 veriyordu hem de önceden token üretmiş herkesin git
  erişimi kırılıyordu. `Store.load()`'a geriye dönük uyumluluk eklendi
  (commit `9c6f63b`).
- `intranetBaseURL` tasarımda `https://intranet.sigortatahkim.org`
  (bare 443) varsayılmıştı; gerçek Intranet-B API'si `:8443`'te
  dinliyor — düzeltildi (commit `ba837f4`).
- Ayrıca bu makinede antivirüs (Trend Micro) `devplatform-login.exe`'yi
  her build'de "Troj.Win32.TRX.XXP" genel sezgisel tespitiyle
  siliyordu (gerçek bir tehdit değil — imzasız, konsol açan, şifre
  okuyan, dışarı bağlanan bir Go binary'sinin klasik yanlış pozitifi).
  `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"` ile build almak
  bu makinede sorunu çözdü; başka bir makinede tekrar ederse aynı build
  bayraklarını dene, olmazsa AV/IT ekibine dosya/klasör istisnası
  gerekecek.
- İlk halinde `/devplatform-login.exe` ve `/devplatform-login/install.ps1`
  route'ları backend'in mux'ında en üst seviyede tanımlanmıştı. Gerçek
  sunucuda frontend'in IIS site'ı sadece `/api`, `/healthz`, `/git`'i
  backend'e reverse-proxy'liyor — bu ikisi o üç öneke girmediği için
  IIS'e hiç ulaşmıyor, SPA'nın kendi client-side router'ına düşüyordu
  (tarayıcı konsolunda "No routes matched" hatası, boş sayfa). İkisi de
  `/api/` altına taşındı (`/api/devplatform-login.exe`,
  `/api/devplatform-login/install.ps1`) — mevcut proxy kuralını
  kullanıyor, IIS tarafında ek bir ayar gerekmiyor.

**Not — bu gerçek sunucuda `devplatform.exe` hâlâ IIS `httpPlatformHandler`
ile çalışıyor, Windows Servisi olarak DEĞİL** (yukarıdaki 2026-08-14
güncellemesi "artık Windows Servisi" diyor ama bu, en azından bu
makinede henüz uygulanmamış — Task Manager'da process olarak görünüyor,
`services.msc`'de yok). Ortam değişkenleri backend'in kendi
`web.config`'indeki `<environmentVariables>` bloğunda
(`D:\inetpub\wwwroot\DevPlatform\Backend\web.config`). Bir değişiklik
sonrası `devplatform.exe`'yi Task Manager'dan sonlandırmak yeterli — IIS
bir sonraki istekte web.config'i okuyup süreci otomatik yeniden
başlatıyor.

## Kontroller

```bash
cd backend  && go build ./... && go vet ./... && go test ./...
cd frontend && npm run build && npm run lint
```

Backend testleri git CLI'ye ihtiyaç duyar (gerçek `git push`/`clone` ile
uçtan uca çalışırlar); git yoksa o testler atlanır.

**Bilinen, koddan bağımsız bir test farkı:** `internal/deploy`'un
`TestBuild_Dotnet_ProducesOutput` testi `testdata/dotnet-fixture`'ı
`net10.0` hedefiyle build ediyor. Makinende daha eski bir .NET SDK varsa
(`dotnet --list-sdks`) bu test NETSDK1045 hatasıyla başarısız olur — bu bir
kod hatası değil, sadece o makinede beklenen SDK'nın kurulu olmaması.
Diğer her şeyi etkilemez.
