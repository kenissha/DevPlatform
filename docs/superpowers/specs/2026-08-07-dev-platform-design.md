# Dev Platform — Tasarım Dokümanı

Tarih: 2026-08-07

## Amaç ve Problem

Proje şu ana kadar tek geliştirici tarafından yürütülüyor; kaynak kod (GitHub), veritabanı bağlantıları, sunucu erişimi ve tüm appsettings/secret bilgileri tamamen bu kişide. Ekibe bir geliştirici daha katılacak. Yeni kişiye:

- Gerçek şifreleri/appsettings'i, sunucu erişimini vermeden,
- Kontrolsüz `git push` / build / deploy yetkisi tanımadan,
- Ama yine de kimin ne üzerinde çalıştığını, hangi işin hangi aşamada olduğunu görebilecek şekilde

birlikte çalışabilmek gerekiyor. Hazır bir araç (GitHub Organization, Jira vb.) satın almak yerine, bu ihtiyacı karşılayan bir platform sıfırdan, kendi kodu olarak yazılacak.

## Kapsam Dışı

- Sunucudaki gerçek production secret'larının/appsettings'in saklandığı nihai yer bu platform değildir — platform bu dosyaları yönetir/enjekte eder ama "gizli bilgi deposu" olarak ayrı, güvenli bir klasör kullanılır (bkz. Faz 2).
- GitHub tamamen terk edilmiyor: gerçek GitHub reposu, sadece sahibinin (Yönetici) periyodik olarak (ayda/6 ayda bir) elle senkronladığı bir "dış arşiv" olarak kalmaya devam ediyor.

## Genel Mimari

Tek bir Go binary olarak yazılır (bağımlılıksız, tek dosya deploy), mevcut sunucuda IIS arkasında reverse proxy ile çalışır (Go kendi portunda HTTP sunar, IIS trafiği ona yönlendirir). Frontend statik dosya olarak aynı binary içine gömülür. Tek SQLite/Postgres veritabanı. 2 kişilik bir ekip için mikroservis mimarisi tercih edilmedi (YAGNI); sistem kod içinde net şekilde ayrılmış modüllerden oluşur:

1. Git barındırma modülü
2. Görev/talep panosu modülü
3. Build/deploy otomasyon modülü
4. Kimlik doğrulama & yetkilendirme modülü

Sadece şirket içi ağ/VPN üzerinden erişilecek şekilde tasarlanır (internete açık değil) — bu, kimlik doğrulama katmanının basit tutulabilmesinin (AD + oturum, 2FA gerekmeden) sebebidir.

## Roller ve Kimlik Doğrulama

- Giriş, Active Directory (AD/LDAP) üzerinden yapılır — platform kendi şifre/hash sistemini yönetmez.
- Başlangıçta 2 sabit rol: **Yönetici** (tam yetkili — onaylar, kişi ekler, secrets yönetir) ve **Geliştirici** (kısıtlı — talep oluşturur, kendine atanan projelere erişir).
- Yetki kontrolleri kod içinde merkezi bir katmandan yapılır (dağınık `if rol == admin` kontrolleri değil), böylece ileride ekip büyüyüp esnek/ayrıntılı bir izin sistemine geçilmesi gerekirse, değişiklik bu tek katmanla sınırlı kalır.
- Proje bazlı erişim: Yönetici, bir geliştiriciye hangi projelere (repolara) erişimi olacağını tek tek atar; geliştirici sadece yetkili olduğu projeleri görür/indirir.

## Faz 1 — Temel: Git barındırma + görev panosu + AD girişi

Bu faz tamamlandığında ikinci kişi GitHub'a hiç dokunmadan, platform üzerinden çalışabilir hale gelir.

- **Git sunucusu:** go-git kütüphanesi ile "smart HTTP" protokolü uygulanır; harici bir git binary'sine bağımlılık yoktur. Standart `git push`/`pull`/`clone` komutları çalışır, hedef GitHub değil bu sunucudur.
- **Branch koruma — protokol seviyesinde:** Korumalı branch'lere (örn. `main`) doğrudan push, git sunucusunun kendisi tarafından reddedilir. Bu bir arayüz kuralı değil, sunucunun push isteğini reddetmesi anlamına gelir — panel arayüzü hiç devrede olmasa da (örn. biri git komutunu terminalden doğrudan çalıştırsa) bu kısıtlama geçerli kalır.
- **Push anında secret taraması:** Gelen her push, bilinen secret/anahtar desenlerine (connection string, API key formatları vb.) karşı taranır; şüpheli bir şey bulunursa push reddedilir/uyarı verilir. Amaç, yanlışlıkla commit edilmiş gerçek bir bilginin iç depoya bile girmesini engellemek.
- **Görev/talep panosu:** Görev oluşturma/atama, durum takibi (yapılıyor / test bekliyor / bitti), acil işaretleme.
- **Merge talebi + inceleme ekranı:** Geliştirici "main'e birleştir" talebi açtığında, Yönetici değişikliğin diff'ini (hangi dosyalar değişti/eklendi/silindi) görür ve ona göre onaylar/reddeder — kör onay yoktur.
- **Audit log:** Görev oluşturma, talep açma, onay/red, deploy, rollback dahil her önemli olay kalıcı ve değiştirilemez şekilde kaydedilir.
- **Bildirim:** Panel içi bildirim + e-posta (yeni görev ataması, onay bekleyen talep, deploy sonucu).

## Faz 2 — Otomasyon: Build/deploy + secrets yönetimi

Bu faz tamamlandığında onaylanan bir talep tek tıkla test veya canlı ortama otomatik olarak taşınır.

- **Ortamlar:** Test ve Canlı olmak üzere 2 ayrı ortam.
- **Secrets deposu:** Sunucuda, git'in tamamen dışında, sadece platformun (ve Yönetici'nin) erişebildiği ayrı bir klasör/depo. `appsettings.Test.json` ve `appsettings.Production.json` (gerçek AD servis bilgisi, gerçek mail/SMTP bilgisi, gerçek veritabanı bağlantıları) burada tutulur; hiçbir zaman git reposuna (ne iç depoya ne GitHub'a) girmez.
- **Deploy akışı (onay sonrası otomatik):**
  1. İlgili commit/versiyon build edilir (`dotnet publish`, `npm run build` vb.) — bu adım hiçbir secret'a ihtiyaç duymaz.
  2. Build çıktısı yeni, versiyonlu bir klasöre yazılır (çalışan canlı klasörün üzerine asla yazılmaz).
  3. Secrets deposundaki ilgili ortamın gerçek appsettings dosyası bu yeni klasöre kopyalanır.
  4. IIS'in ilgili site/app pool'unun fiziksel yolu bu yeni klasöre çevrilir (atomic swap) — canlı sistem, build sırasında kesintiye uğramaz; sadece swap anında birkaç saniyelik olağan app pool yeniden başlatması yaşanır.
- **Versiyon saklama:** Her ortam için son 5-10 versiyon diskte tutulur, daha eskisi otomatik silinir.
- **Rollback:** Saklanan eski versiyonlardan biri seçilip tek tıkla aktif hale getirilebilir.
- **Güvenlik:** Build/deploy komutları hiçbir zaman kullanıcıdan gelen serbest metinle oluşturulmaz; proje/branch/ortam seçimleri sabit listeden yapılır. Build/deploy işlemini çalıştıran servis hesabı en az yetki ilkesiyle sınırlıdır (yalnızca kendi klasörlerine erişebilir).

## Faz 3 — Genişleme: Personel entegrasyonu + yedekleme

Bu faz tamamlandığında kişi ekleme ve veri koruma da sisteme dahil olur.

- **Kişi ekleme:** Yönetici, mevcut kurumsal personel veritabanından kişi seçerek platforma ekler; kişiye davet e-postası gider, AD kimliğiyle giriş yapar.
- **Proje bazlı yetkilendirme:** Yönetici, eklenen kişiye hangi projelere erişimi olacağını (görme/indirme) tek tek atar.
- **İç git deposu yedeği:** Git deposunun gecelik otomatik yedeği alınır (sunucuda ayrı bir disk/konum ya da başka bir makineye kopyalama). Gerekçe: gerçek GitHub'a senkron seyrek (ayda/6 ayda bir) yapıldığı için, aradaki dönemde iç depo tek kopya durumundadır; sunucu veri kaybı riskine karşı yedek gereklidir.

## Bu Platformun Dışında Kalan, Ana Projede Yapılması Gereken Değişiklik

Bu tasarımın çalışabilmesi için, geliştirilen asıl projelerin (örn. mevcut backend/frontend) kendi appsettings yapılarını ortama göre ayırması gerekir:

- `appsettings.Development.json` (yerelde, git'e girmez): gerçek AD yerine sahte/otomatik test kullanıcısı girişi, gerçek mail sunucusu yerine sahte/loglayan mail gönderici, gerçek olmayan (test) veritabanı bağlantısı kullanılır.
- `appsettings.Test.json` / `appsettings.Production.json`: gerçek AD, gerçek mail, gerçek veritabanı bilgileri içerir; sadece sunucudaki secrets deposunda bulunur, deploy anında enjekte edilir.

Bu değişiklik bu platformun kapsamı dışında ama önkoşuludur; ayrı bir iş olarak ele alınmalıdır.

## Repo Yapısı ve Frontend

- Proje adı **DevPlatform**, tek repo (monorepo) olarak `https://github.com/kenissha/DevPlatform` üzerinde tutulur.
- Repo kökünde `backend/` (Go) ve `frontend/` (React) ayrı klasörler olarak durur.
- Gerekçe: Frontend ve backend bağımsız sürümlenmiyor — frontend build çıktısı backend binary'sine gömülüyor (bkz. Genel Mimari), tek ürün olarak birlikte geliştirilip birlikte sürüm alınıyor. İki repo açmak bu proje için ekstra karmaşıklık, gerçek bir fayda getirmiyor.
- Frontend: **React**. Backend'in embed ettiği statik dosya çıktısını üretir (build sonrası `frontend/dist` gibi bir klasör, Go tarafından `embed` ile binary'ye gömülür).

## Açık Kararlar / Notlar

- Test ortamı için gerekli ayrı veritabanları (IntranetDB test, vb.) zaten mevcut; ek kurulum gerekmiyor.
- Rol → izin eşlemesi şu an elle yapılacak (AD grubundan otomatik türetme değil).
