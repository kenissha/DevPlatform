package gittoken

import "testing"

func TestGenerate_ProducesVerifiableToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	token, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if token == "" {
		t.Fatal("Generate returned an empty token")
	}
	if !store.Verify("dev-1", token) {
		t.Error("Verify(subject, correct token) = false, want true")
	}
}

func TestGenerate_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if _, err := store.Generate(""); err != ErrInvalidSubject {
		t.Errorf("Generate(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestGenerate_RegeneratingInvalidatesThePreviousToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	first, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	second, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}
	if first == second {
		t.Fatal("two calls to Generate produced the same token")
	}
	if store.Verify("dev-1", first) {
		t.Error("Verify still accepts the token that Generate replaced")
	}
	if !store.Verify("dev-1", second) {
		t.Error("Verify rejects the current token")
	}
}

func TestVerify_RejectsWrongToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	if _, err := store.Generate("dev-1"); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if store.Verify("dev-1", "not-the-real-token") {
		t.Error("Verify accepted a wrong token")
	}
}

func TestVerify_RejectsUnknownSubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if store.Verify("nobody", "any-token") {
		t.Error("Verify accepted a subject with no stored token")
	}
}

func TestRevoke_InvalidatesTheToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if err := store.Revoke("dev-1"); err != nil {
		t.Fatalf("Revoke returned error: %v", err)
	}
	if store.Verify("dev-1", token) {
		t.Error("Verify still accepts a token after Revoke")
	}
}

func TestRevoke_NonexistentSubjectIsNotAnError(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke("nobody"); err != nil {
		t.Errorf("Revoke on a subject with no token returned error: %v", err)
	}
}

func TestRevoke_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke(""); err != ErrInvalidSubject {
		t.Errorf("Revoke(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/git-tokens.json"
	store1 := NewStore(path)
	token, err := store1.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	store2 := NewStore(path)
	if !store2.Verify("dev-1", token) {
		t.Error("a fresh Store instance backed by the same file does not see the earlier Generate")
	}
}
