# IIS Yardımcı Servisi — Yetki Ayrımı Tasarım Dokümanı

Tarih: 2026-08-13

## Amaç ve Problem

`internal/deployment` (panelden deploy onayı) canlıya alınmadan önce yapılan güvenlik incelemesinde bulunan, kod düzeltmesiyle çözülemeyecek bir mimari açık: `deploy.Pipeline`, bir deploy'un build adımını (`npm run build`/`dotnet publish` — reponun kendi script'leri, tamamen güvenilmeyen içerik) ve IIS swap adımını (appcmd.exe, Administrator yetkisi gerektirir) **aynı süreçte, aynı yetkiyle** çalıştırıyor. `appcmd`'nin çalışabilmesi için bütün `devplatform.exe` sürecinin Administrator yetkisiyle çalışması gerekiyor demek — yani repoya push edip bir deploy onayı alabilen biri, o repo içine build sırasında çalışacak kötü niyetli bir script koyarsa, host üzerinde fiilen Administrator koduna ulaşır.

Hedef: `devplatform.exe`'nin (git barındırma, panel API'si, build dahil) **hiçbir zaman** yükseltilmiş yetkiyle çalışmasına gerek kalmaması; appcmd'yi gerçekten çalıştıran tek şeyin, çok dar ve sabit bir işe (IIS site'ının fiziksel yolunu değiştirmek) kilitlenmiş, ayrı bir yardımcı servis olması.

## Kapsam Dışı

- IIS site'ı **oluşturma** (`New-WebSite` vb.) bu tasarımın kapsamında değil — kullanıcı bunu bugüne kadar olduğu gibi elle, kendi Yönetici PowerShell oturumundan yapmaya devam edecek. Kod tabanında appcmd'ye giden tek çağrı zaten sadece `IISSwapper.SetPhysicalPath` (fiziksel yol değiştirme); başka appcmd işlemi yok, bu tasarım da yenisini eklemiyor.
- `devplatform.exe`'nin kendisinin Windows Service olarak paketlenmesi bu tasarımın konusu değil — bugün nasıl çalıştırılıyorsa (elle başlatılan bir binary) öyle kalabilir; tek fark artık hiçbir zaman Yönetici olarak başlatılmasına gerek olmaması.
- DevPlatform ile IIS her zaman aynı makinede olacak (kullanıcı onayladı) — bu yüzden makineler arası (ağ üzerinden) bir iletişim tasarlanmıyor.

## Genel Mimari

Bugün appcmd'yi çağıran tek nokta — `internal/deploy/iisswap.go`'daki `IISSwapper.SetPhysicalPath`, `CommandRunner` arayüzü üzerinden çalışıyor:

```go
type CommandRunner interface {
    Run(name string, args ...string) ([]byte, error)
}
```

Bu arayüz zaten testler için sahte bir çalıştırıcı kullanılabilsin diye soyutlanmış — yetki ayrımı için de aynı sınır kullanılıyor, `IISSwapper`'ın kendisinde **hiçbir değişiklik yok**.

İki süreç:

1. **`devplatform.exe`** (mevcut, değişmeyen ana süreç) — git, panel API'si, build, versiyon yönetimi. `RealCommandRunner{}` yerine yeni `HelperCommandRunner{}` kullanır. Hiçbir zaman Administrator olarak çalışmaz.
2. **`iishelper.exe`** (yeni, küçük, ayrı binary) — Windows Service olarak kurulur, `LocalSystem` hesabıyla çalışır (appcmd'nin ihtiyaç duyduğu yetkiye zaten sahip, ayrı bir admin hesabı yönetmeye gerek yok). Yerel bir named pipe'ı dinler. Tek işi var: gelen isteğin tam olarak beklenen tek şekle uyup uymadığını kendi kendine doğrulamak, uyuyorsa gerçekten çalıştırmak.

## Bileşenler

- **`backend/internal/iishelper/protocol.go`** — hem `devplatform.exe` hem `iishelper.exe` tarafından import edilen, pipe üzerinden geçen isteğin/cevabın JSON şekli. Sabit, küçük bir sözleşme:
  ```go
  type Request struct {
      Name string   // her zaman appcmd.exe'nin mutlak yolu
      Args []string // her zaman ["set", "vdir", "<site>/", "/physicalPath:<mutlak yol>"]
  }
  type Response struct {
      Output []byte
      Error  string // boşsa başarılı
  }
  ```
- **`backend/internal/iishelper/client.go`** — `HelperCommandRunner`, `deploy.CommandRunner` arayüzünü uygular. `Run(name, args...)` pipe'a bağlanır, `Request`'i yollar, `Response`'u okur, `Error` doluysa `error` olarak döner. Sabit bir zaman aşımı (30 saniye) — pipe/servis takılırsa deploy sonsuza kadar asılı kalmaz.
- **`backend/internal/iishelper/server.go`** — pipe sunucusunun çekirdek mantığı (Windows Service sarmalayıcısından ayrı, böylece testlerde gerçek servis kurulumu gerekmeden test edilebilir). Her bağlantıda: `Request`'i oku → **doğrula** (aşağıya bakınız) → geçerliyse `deploy.RealCommandRunner{}` ile çalıştır → `Response` yaz → bağlantıyı kapat.
  - **Doğrulama — asıl güvenlik sınırı burası:** `Name` tam olarak `iishelper` kendi ortamından çözdüğü appcmd yoluna eşit olmalı (çağıranın gönderdiği isme güvenilmez, sunucu appcmd yolunu kendisi de bağımsızca hesaplar ve karşılaştırır); `Args` tam olarak `len==4 && Args[0]=="set" && Args[1]=="vdir" && Args[2]` bilinen bir site listesindeki (deploy targets dosyasındaki `SiteName` değerleri) bir site adı + `"/"` && `Args[3]` `"/physicalPath:"` ön ekiyle başlayıp geri kalanı mutlak bir yol olmalı. Bu şekle uymayan **hiçbir** istek çalıştırılmaz, açık bir hata döner.
- **`backend/cmd/iishelper/main.go`** — `golang.org/x/sys/windows/svc` ile Windows Service yaşam döngüsü (Start/Stop/Shutdown). `iishelper.NewServer(...)`'ı kurar, pipe'ı açar.
- **`backend/cmd/devplatform/main.go`** — tek satır değişiklik: `deploy.NewIISSwapper(deploy.RealCommandRunner{})` yerine `deploy.NewIISSwapper(iishelper.NewHelperCommandRunner())`.

## Veri Akışı

Deploy onaylanır → `deployment.Handlers.Approve` → `deploy.Pipeline.Deploy` → `IISSwapper.SetPhysicalPath(siteName, releaseDir)` (değişmedi, hâlâ kendi mutlak-yol kontrolünü yapıyor) → `runner.Run(appcmdPath(), "set", "vdir", ...)` artık `HelperCommandRunner.Run` → pipe üzerinden `iishelper`'a gider → doğrulanır, çalıştırılır → sonuç aynı yoldan geri döner → `deployment` paketindeki mevcut hata sınıflandırması ("IIS activation failed") hiç değişmeden aynı şekilde çalışır.

## Erişim Kontrolü

Named pipe, Windows'un kendi SDDL (güvenlik tanımlayıcı) mekanizmasıyla açılır: sadece `devplatform.exe`'nin çalıştığı hesaba bağlanma izni verilir, `Everyone`/diğer yerel kullanıcılara açıkça reddedilir. Böylece aynı makinedeki başka bir düşük yetkili kullanıcı/süreç pipe'a bağlanıp appcmd'yi tetikleyemez — sadece devplatform.exe'nin kendi hesabı.

Bu, `devplatform.exe`'nin **bilinen, sabit bir hesap altında** çalışmasını gerektirir (SDDL bir hesabı adıyla/SID'iyle hedef alır). Uygulama planında, gerçek sunucuya kurulum adımı olarak bunun nasıl ayarlanacağı somutlaştırılacak; bu dev makinede bugüne kadar hangi hesapla çalıştırıldıysa (kullanıcının kendi oturumu) o hesap hedeflenebilir, gerçek sunucuda ayrı bir servis hesabı oluşturmak iyi pratik olur ama bu tasarımın zorunlu bir parçası değil.

## Hata Durumları

- **Servis kapalı/kurulu değil:** pipe'a bağlanma başarısız olur → `HelperCommandRunner.Run` açık bir hata döner ("iishelper servisine ulaşılamadı") → mevcut `deployment` hata sınıflandırmasında "IIS activation failed" olarak panelde görünür, ham detay sadece sunucu logunda.
- **İstek doğrulanamadı (olması beklenmez, savunma amaçlı):** `iishelper` isteği reddeder, `Response.Error` doldurulur, aynı hata yoluyla panele yansır.
- **Pipe/servis takılırsa:** 30 saniyelik sabit zaman aşımı zaten var olan deploy-seviyesi zaman aşımına (10 dakika) ek bir iç güvenlik ağı.

## Test

- `iishelper.server`'ın doğrulama mantığı: izin verilen tek şekli kabul ettiğini, her sapmayı (yanlış site adı, göreli yol, fazla/eksik argüman, farklı appcmd yolu, path-traversal içeren physicalPath) reddettiğini kanıtlayan tablo-tabanlı testler — asıl güvenlik sınırı burada, en ayrıntılı test kapsamı buraya.
- `HelperCommandRunner`: gerçek bir named pipe üzerinden, testte kurulan sahte bir `iishelper.server`'a karşı test edilir (gerçek appcmd'ye hiç dokunmadan) — istek/cevap kodlaması, zaman aşımı, servis kapalıyken hata döndüğü.
- Mevcut `deploy`/`deployment` test paketleri **değişmeden** geçerli kalır — onlar zaten sahte bir `CommandRunner` kullanıyor, `HelperCommandRunner` sadece production'da kullanılan yeni bir gerçek implementasyon.
- **Gerçek uçtan uca doğrulama (bu makinede):** `iishelper` servisi gerçekten kurulup, mevcut test IIS site'ına (`DevPlatform Test Site`) karşı, `devplatform.exe` hiç yükseltilmeden çalışırken gerçek bir deploy denenir — ilk IIS kanıtlamasında yapıldığı gibi.

## Kurulum (bir kere, script'le)

Uygulama planı bir PowerShell script'i içerecek: `iishelper.exe`'yi Windows Service olarak kaydeder (`sc.exe create`, otomatik başlangıç, `LocalSystem` hesabı), named pipe'ın SDDL'ini `devplatform.exe`'nin çalıştığı hesaba göre ayarlar.
