# Process Tabanlı Backend Deploy — Tasarım Dokümanı

Tarih: 2026-08-19

## Amaç ve Problem

Bugün ilk kez gerçek bir canlı deploy'u (OASRapor'un React frontend'i) uçtan uca test ettik ve çalıştı: `IISSwapper.SetPhysicalPath` bir IIS site'ının fiziksel yolunu değiştiriyor, statik dosyalar anında yeni klasörden servis edilmeye başlıyor. Ama bu mekanizma **sadece statik içerik için** doğru çalışıyor.

Bir .NET (veya Go) backend'i, IIS altında `httpPlatformHandler` üzerinden başlatılan **sürekli çalışan bir process**'tir (`devplatform.exe`'nin kendisi ve OASRapor'un Go backend'i tam olarak bu şekilde çalışıyor). Kullanıcının kendi tecrübesiyle doğruladığı gibi: bu process, kendi çalıştırılabilir dosyasını (ve altındaki dosyaları) **kilitler**. Sadece `physicalPath`'i değiştirmek hiçbir şey yapmaz — eski process, eski dosyalarla çalışmaya devam eder; yeni klasöre yazmaya çalışmak da zaten kilit yüzünden başarısız olabilir. Process'i **önce durdurmadan** ne yeni dosyaları güvenle yerine koyabiliriz ne de IIS'in yeni klasörden gerçekten yeni bir process başlatmasını sağlayabiliriz.

Hedef: `dotnet` tarifiyle (recipe) build edilen deploy hedefleri için, mevcut versiyonlu-klasör + rollback modelini koruyarak, process'i güvenle durdurup yeniden başlatan bir akış eklemek.

## Kapsam Dışı

- **Go build tarifi** — ayrı, sonraki bir iş. Bu tasarım sadece zaten var olan `dotnet` tarifini process-farkındalıklı hale getiriyor.
- **IIS site'ı/uygulama havuzu oluşturma** — önceki tasarımda olduğu gibi kapsam dışı, kullanıcı elle kuruyor.
- Statik (`npm`) hedeflerin akışı **hiç değişmiyor** — bu tasarım sadece `dotnet` tarifini etkiliyor.

## Genel Yaklaşım

`Recipe` alanı zaten her deploy hedefinde var (`dotnet` | `npm`). Yeni davranış tamamen buna göre dallanıyor:

- `Recipe == npm` → bugünkü akış, değişiklik yok (sadece `SetPhysicalPath`).
- `Recipe == dotnet` → build **process'i durdurmadan önce** biter (yeni, boş bir klasöre yazıldığı için canlıdaki process'e hiç dokunmuyor); sonra sırasıyla **durdur → yeni klasöre çevir → başlat**.

Bu, kesintiyi build süresinden ayırıp sadece durdur/çevir/başlat penceresine sıkıştırıyor — build dakikalar sürebilir, kesinti saniyeler sürmeli.

### Başlatma başarısız olursa: otomatik geri dönüş

En riskli an: durdurma ve klasör değişimi başarılı ama **yeni process başlamıyor** (bozuk build, çalışma zamanı hatası, vb.). Bu durumda site'ı kapalı bırakmak yerine, **aynı gün eklediğimiz rollback mekanizmasını** kendi içinde kullanıyoruz:

1. Yeni deploy başlamadan önce, o an aktif olan release klasörü not edilir (`activeRelease` — Releases/Rollback'in zaten kullandığı fonksiyon).
2. Durdur → yeni klasöre çevir → başlat.
3. Başlatma **başarısız olursa**: otomatik olarak eski (bir önceki aktif) klasöre geri çevrilir, tekrar başlatılır.
4. Bu geri dönüş **başarılı olursa**: deploy `StatusFailed` olarak işaretlenir (istenen yeni kod çalışmıyor) ama site kesintisiz, eski haliyle çalışmaya devam eder. Panelde ayrı, net bir hata mesajı gösterilir: *"Yeni versiyon başlatılamadı, otomatik olarak önceki çalışan versiyona dönüldü."*
5. Bu geri dönüş de **başarısız olursa** (iki ayrı hata art arda — nadir): site gerçekten erişilemez durumda demektir. Bu, diğerlerinden görsel olarak ayrılan, en yüksek önemde bir hata mesajıyla bildirilir: *"[site adı] durduruldu ve yeniden başlatılamadı — site şu an ERİŞİLEMEZ, elle müdahale gerekiyor."*

Her iki başarısız durumda da (otomatik geri dönüş başarılı ya da başarısız) `Prune` (eski versiyonları temizleme adımı) **hiç çalışmaz** — bugünkü akışta olduğu gibi, sadece başarıyla aktive edilmiş yeni bir release'in ardından çalışıyor. Başarısız bir deploy'da zaten yeni bir şey "aktive" olmadığı için temizlenecek bir şey yok; bu da otomatik geri dönüşün ihtiyaç duyabileceği eski klasörün yanlışlıkla silinmesini engelliyor.

Rollback (`Handlers.Rollback`) da aynı durdur→çevir→başlat sarmalamasını, `target.Recipe == dotnet` olduğunda uygular — geriye dönerken de process'i öldürüp yeniden başlatmadan eski klasöre güvenle geçilemez.

## Bileşenler

### `internal/iishelper` — yeni izinli işlemler

`ValidateRequest`, şu an tek bir şekli (`set vdir .../physicalPath:...`) kabul ediyor. İki yeni şekil ekleniyor:

```
appcmd stop site /site.name:"<site>"
appcmd start site /site.name:"<site>"
```

Güvenlik sınırı **aynı kalıyor**: her iki yeni komut da `<site>`'ın ops-managed izin listesinde (`allowed-sites.json`) olmasını zorunlu kılıyor — üç komuttan hiçbiri onaylanmamış bir site'a dokunamıyor. `iishelper` hangi site'ın "process tabanlı" olduğunu hiç bilmiyor/umursamıyor; bu karar tamamen `deployment` paketinde, `Recipe`'e bakılarak veriliyor — `iishelper` sadece "şekil ve site izinli mi" diye bakan sabit bir kapı bekçisi olmaya devam ediyor.

**Not — canlıda doğrulanacak:** appcmd'nin `stop site`/`start site` için gerçek argüman söz dizimi (`/site.name:` biçimi) burada belgelere dayanıyor, bugünkü SDDL/ApplicationPoolIdentity keşiflerinde olduğu gibi gerçek sunucuda ilk canlı testte kesinleştirilecek — birim testleri sahte çalıştırıcıyla yazıldığı için bu detay onları etkilemiyor, sadece gerçek appcmd çağrısını.

**Kabul edilen yeni risk:** `iishelper` artık izinli bir site'ı tamamen **durdurabiliyor** — önceden en kötü ihtimal "yanlış klasörü göster"ken, artık "kesinti yarat" da mümkün. Bu, özelliğin doğası gereği ortadan kaldırılamayan ama DevPlatform'un iç, ağa kapalı bir araç olması nedeniyle (bkz. kullanıcıyla yapılan tartışma) orantılı kabul edilen bir risk — ek bir Recipe-bazlı kısıtlama eklenmiyor, mevcut site izin listesi + her stop/start'ın audit'e düşmesi yeterli görülüyor.

### `internal/deploy` — `IISSwapper`'a iki yeni metod

```go
func (s *IISSwapper) StopSite(siteName string) error
func (s *IISSwapper) StartSite(siteName string) error
```

`SetPhysicalPath`'in yanına eklenir, aynı `CommandRunner` arayüzünü kullanır — testlerde aynı sahte çalıştırıcıyla (`fakeCommandRunner`) doğrulanabilir.

### `internal/deploy` — `Pipeline.Deploy`'a Recipe-farkında dal

`Pipeline.Deploy`, `recipe == RecipeDotnet` olduğunda build sonrası şu sırayı izler: `StopSite` → `SetPhysicalPath` → `StartSite`. `StartSite` hata dönerse, otomatik geri dönüş denemesi (eski releaseDir'e `SetPhysicalPath` + `StartSite`) yapılır. Geri dönüş de başarısız olursa, iki hatayı da taşıyan, "site erişilemez" durumunu ayırt eden özel bir hata türü (`ErrSiteDown` gibi) döner — `deployment` paketindeki `failureReason` bunu tanıyıp ayrı, yüksek öncelikli bir mesaja çevirir.

### `internal/deployment` — `Approve` ve `Rollback`

Her iki handler da `target.Recipe`'e bakıp Pipeline'a/IISSwapper'a bu bilgiyi taşır (Approve zaten `Pipeline.Deploy`'a recipe'i geçiyor, değişiklik yok; Rollback şu an sadece `SetPhysicalPath` çağırıyor, `target.Recipe == RecipeDotnet` olduğunda bunun yerine aynı durdur→çevir→başlat+otomatik-geri-dönüş mantığını çağıracak şekilde genişler).

`failureReason` fonksiyonuna yeni bir sınıflandırma eklenir: normal "IIS yayınlama başarısız" ile "otomatik geri dönüldü, site ayakta" ile "site erişilemez, acil" birbirinden ayrılır.

## Test Stratejisi

Mevcut desenle birebir aynı: `fakeCommandRunner` ile gerçek appcmd'ye hiç dokunmadan — durdur/çevir/başlat çağrılarının doğru sırayla geldiğini, başlatma hatasında geri dönüşün tetiklendiğini, geri dönüş de başarısız olduğunda özel hatanın döndüğünü doğrulayan birim testleri. `iishelper.ValidateRequest`'in yeni iki şekli kabul ettiğini, izinsiz site için reddettiğini doğrulayan testler (mevcut `validate_test.go` deseninde).

Gerçek sunucuda canlı doğrulama: önemsiz, düşük riskli bir dotnet/Go backend'i ile (bugün React'te yaptığımız gibi) — bu tasarımın kapsamında değil, ayrı bir uygulama adımı.
