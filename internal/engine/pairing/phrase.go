package pairing

import (
	"crypto/hkdf"
	"crypto/sha256"
	_ "embed"
	"io"
	"math/big"
	"strings"
)

// PhraseScalar is the SPAKE2 password scalar w of a phrase's words: HKDF-SHA256
// of the words joined by single spaces, 40 bytes reduced mod the group order
// (64 bits more than the order, as RFC 9382 § 3.2 suggests).
func PhraseScalar(words []string) []byte {
	ikm := strings.Join(words, " ")
	out, err := hkdf.Key(sha256.New, []byte(ikm), []byte("legato pair v1"), "spake2 w", 40)
	if err != nil {
		panic(err)
	}
	w := new(big.Int).Mod(new(big.Int).SetBytes(out), order)
	return scalarBytes(w)
}

// The EFF short word list 2.0 (CC BY 3.0 US, eff.org/dice), with "yo-yo"
// written "yoyo". Every word is known by its first three letters.
//
//go:embed words.txt
var wordList string

// Words is the word list phrases are drawn from.
func Words() []string {
	return strings.Fields(wordList)
}

// NewPhrase draws four words, each uniformly from the list.
func NewPhrase(random io.Reader) ([]string, error) {
	words := Words()
	n := len(words)
	limit := (65536 / n) * n
	phrase := make([]string, 0, 4)
	buf := make([]byte, 2)
	for len(phrase) < 4 {
		if _, err := io.ReadFull(random, buf); err != nil {
			return nil, err
		}
		v := int(buf[0])<<8 | int(buf[1])
		if v >= limit {
			continue
		}
		phrase = append(phrase, words[v%n])
	}
	return phrase, nil
}
