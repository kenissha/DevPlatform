package taskboard

import (
	"reflect"
	"testing"
)

func TestKeyRefsIn(t *testing.T) {
	tests := []struct {
		name    string
		message string
		prefix  string
		want    []string
	}{
		{"tek anahtar", "DEN-14 tarih filtresi düzeltildi", "DEN", []string{"DEN-14"}},
		{"küçük harf", "den-14 düzeltildi", "DEN", []string{"DEN-14"}},
		{"cümlenin ortasında", "fix: bunu DEN-7 için yaptım", "DEN", []string{"DEN-7"}},
		{"parantez içinde", "temizlik (DEN-3)", "DEN", []string{"DEN-3"}},
		{"iki farklı anahtar", "DEN-1 ve DEN-2 birlikte", "DEN", []string{"DEN-1", "DEN-2"}},
		{"aynı anahtar iki kez", "DEN-5: ...\n\nDEN-5 tamamlandı", "DEN", []string{"DEN-5"}},
		{"gövdede", "başlık\n\nRefs DEN-9", "DEN", []string{"DEN-9"}},
		{"çok haneli", "DEN-1234 bitti", "DEN", []string{"DEN-1234"}},

		{"başka reponun anahtarı", "OAS-3 düzeltildi", "DEN", nil},
		{"sayı yok", "DEN- bir şey", "DEN", nil},
		{"tire yok", "DEN14 bir şey", "DEN", nil},
		{"kelimenin içinde", "GARDEN-14 bahçe", "DEN", nil},
		{"sonrasında harf", "DEN-14b nedir", "DEN", nil},
		{"boş mesaj", "", "DEN", nil},
		{"prefix yok", "DEN-14", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeyRefsIn(tt.message, tt.prefix)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("KeyRefsIn(%q, %q) = %v, want %v", tt.message, tt.prefix, got, tt.want)
			}
		})
	}
}

// The prefix a repo actually uses can have been lengthened past what
// keyPrefix would compute (INT vs INTR), so it is read from the registry.
func TestPrefixForReadsTheAllocatedPrefixNotAFreshGuess(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Create("intranet-backend", "Bir", "", "", "dev-1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Create("intranet-frontend", "İki", "", "", "dev-1"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := store.PrefixFor("intranet-backend")
	if err != nil {
		t.Fatalf("PrefixFor: %v", err)
	}
	second, err := store.PrefixFor("intranet-frontend")
	if err != nil {
		t.Fatalf("PrefixFor: %v", err)
	}
	if first == second {
		t.Fatalf("both repos got prefix %q", first)
	}
	if first != "INT" {
		t.Fatalf("first repo prefix = %q, want INT", first)
	}
}

func TestPrefixForUnknownRepoIsEmpty(t *testing.T) {
	store := NewStore(t.TempDir())

	prefix, err := store.PrefixFor("bos")
	if err != nil {
		t.Fatalf("PrefixFor: %v", err)
	}
	if prefix != "" {
		t.Fatalf("prefix = %q, want empty", prefix)
	}
}
