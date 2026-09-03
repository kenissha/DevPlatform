package gittoken

import "testing"

func TestGenerate_ProducesVerifiableToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	_, token, err := store.Generate("dev-1", "test label")
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

	if _, _, err := store.Generate("", "label"); err != ErrInvalidSubject {
		t.Errorf("Generate(\"\", ...) error = %v, want ErrInvalidSubject", err)
	}
}

func TestGenerate_DoesNotInvalidateAPreviousToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	_, first, err := store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	_, second, err := store.Generate("dev-1", "desktop")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}
	if first == second {
		t.Fatal("two calls to Generate produced the same token")
	}
	if !store.Verify("dev-1", first) {
		t.Error("Verify rejects the first token after a second was generated — Generate must not invalidate previous tokens")
	}
	if !store.Verify("dev-1", second) {
		t.Error("Verify rejects the second token")
	}
}

func TestVerify_RejectsWrongToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	if _, _, err := store.Generate("dev-1", "label"); err != nil {
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

func TestList_ReturnsTokensNewestFirstWithoutHashes(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	if _, _, err := store.Generate("dev-1", "laptop"); err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	if _, _, err := store.Generate("dev-1", "desktop"); err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}

	list, err := store.List("dev-1")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d tokens, want 2", len(list))
	}
	if list[0].Label != "desktop" || list[1].Label != "laptop" {
		t.Errorf("labels in order = [%q, %q], want [desktop, laptop] (newest first)", list[0].Label, list[1].Label)
	}
}

func TestList_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if _, err := store.List(""); err != ErrInvalidSubject {
		t.Errorf("List(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestRevoke_InvalidatesOnlyTheMatchingToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	id1, token1, err := store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	_, token2, err := store.Generate("dev-1", "desktop")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}

	if err := store.Revoke("dev-1", id1); err != nil {
		t.Fatalf("Revoke returned error: %v", err)
	}
	if store.Verify("dev-1", token1) {
		t.Error("Verify still accepts the revoked token")
	}
	if !store.Verify("dev-1", token2) {
		t.Error("Revoke invalidated a token it wasn't asked to")
	}
}

func TestRevoke_NonexistentIDIsNotAnError(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke("dev-1", "no-such-id"); err != nil {
		t.Errorf("Revoke on an unknown id returned error: %v", err)
	}
}

func TestRevoke_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke("", "some-id"); err != ErrInvalidSubject {
		t.Errorf("Revoke(\"\", ...) error = %v, want ErrInvalidSubject", err)
	}
}

func TestRevokeAll_InvalidatesEveryToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	_, token1, err := store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	_, token2, err := store.Generate("dev-1", "desktop")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}

	if err := store.RevokeAll("dev-1"); err != nil {
		t.Fatalf("RevokeAll returned error: %v", err)
	}
	if store.Verify("dev-1", token1) || store.Verify("dev-1", token2) {
		t.Error("RevokeAll left at least one token still valid")
	}
}

func TestRevokeAll_NonexistentSubjectIsNotAnError(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.RevokeAll("nobody"); err != nil {
		t.Errorf("RevokeAll on a subject with no tokens returned error: %v", err)
	}
}

func TestRevokeAll_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.RevokeAll(""); err != ErrInvalidSubject {
		t.Errorf("RevokeAll(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/git-tokens.json"
	store1 := NewStore(path)
	_, token, err := store1.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	store2 := NewStore(path)
	if !store2.Verify("dev-1", token) {
		t.Error("a fresh Store instance backed by the same file does not see the earlier Generate")
	}
}
