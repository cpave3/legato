package pairing

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The app keeps its own copy of the word list; both must hash to this.
const wordListDigest = "79fb63e0b81dfda90fea676b7080a352ce560178360dce20a18e8f49f7570dcf"

func TestWordListIsTheSharedOne(t *testing.T) {
	sum := sha256.Sum256([]byte(strings.Join(Words(), "\n") + "\n"))
	if got := hex.EncodeToString(sum[:]); got != wordListDigest {
		t.Fatalf("word list digest = %s", got)
	}
}

func TestEveryWordIsKnownByItsFirstThreeLetters(t *testing.T) {
	words := Words()
	if len(words) != 1296 {
		t.Fatalf("%d words", len(words))
	}
	seen := map[string]string{}
	for _, w := range words {
		if other, ok := seen[w[:3]]; ok {
			t.Fatalf("%s and %s share a prefix", w, other)
		}
		seen[w[:3]] = w
	}
}

func TestNewPhraseIsFourWordsFromTheList(t *testing.T) {
	known := map[string]bool{}
	for _, w := range Words() {
		known[w] = true
	}
	phrase, err := NewPhrase(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(phrase) != 4 {
		t.Fatalf("phrase %v", phrase)
	}
	for _, w := range phrase {
		if !known[w] {
			t.Fatalf("%q is not a list word", w)
		}
	}
}

func TestNewPhraseDrawsEachWordUniformly(t *testing.T) {
	// Two random bytes give 0..65535; 65535 >= 50*1296 is rejected, so a
	// reader of 0xffff then 0x0000 yields the first word.
	random := bytes.NewReader([]byte{0xff, 0xff, 0, 0, 0, 1, 0, 2, 0x05, 0x10})
	phrase, err := NewPhrase(random)
	if err != nil {
		t.Fatal(err)
	}
	words := Words()
	want := []string{words[0], words[1], words[2], words[0x0510%1296]}
	if strings.Join(phrase, " ") != strings.Join(want, " ") {
		t.Fatalf("phrase %v, want %v", phrase, want)
	}
}
