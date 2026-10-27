package pairing

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestSealedTextOpensWithTheSameKeyAndLabel(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 16)
	sealed, err := Seal(key, LabelServer, []byte(`{"token":"abc"}`), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(key, LabelServer, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"token":"abc"}` {
		t.Fatalf("opened %q", got)
	}
}

func TestSealedTextDoesNotOpenWithAnotherKeyOrLabel(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 16)
	sealed, _ := Seal(key, LabelServer, []byte("secret"), rand.Reader)
	if _, err := Open(bytes.Repeat([]byte{8}, 16), LabelServer, sealed); err == nil {
		t.Fatal("opened with another key")
	}
	if _, err := Open(key, LabelPhone, sealed); err == nil {
		t.Fatal("opened under the other direction's label")
	}
	if _, err := Open(key, LabelServer, sealed[:5]); err == nil {
		t.Fatal("opened a truncated text")
	}
}
