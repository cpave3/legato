package pairing

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateFixture = flag.Bool("update", false, "rewrite testdata/exchange.json")

// One whole exchange with fixed randomness. The app's tests read the same file
// (mobile/app/src/test/resources/pairing-exchange.json) and must reach the same
// values, which is what proves the two sides speak the same protocol.
type exchangeFixture struct {
	Phrase          string `json:"phrase"`
	W               string `json:"w"`
	X               string `json:"x"`
	Y               string `json:"y"`
	ShareA          string `json:"share_a"`
	ShareB          string `json:"share_b"`
	Key             string `json:"key"`
	ConfirmA        string `json:"confirm_a"`
	ConfirmB        string `json:"confirm_b"`
	PhonePlaintext  string `json:"phone_plaintext"`
	PhoneSealed     string `json:"phone_sealed"`
	ServerPlaintext string `json:"server_plaintext"`
	ServerSealed    string `json:"server_sealed"`
}

func buildFixture(t *testing.T) exchangeFixture {
	phrase := []string{"river", "candle", "otter", "mint"}
	x := unhex(t, "43dd0fd7215bdcb482879fca3220c6a968e66d70b1356cac18bb26c84a78d729")
	y := unhex(t, "dcb60106f276b02606d8ef0a328c02e4b629f84f89786af5befb0bc75b6e66be")
	w := PhraseScalar(phrase)
	a, shareA, err := Start(PartyA, Protocol, w, bytes.NewReader(x))
	if err != nil {
		t.Fatal(err)
	}
	b, shareB, err := Start(PartyB, Protocol, w, bytes.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	keysA, err := a.Finish(shareB)
	if err != nil {
		t.Fatal(err)
	}
	keysB, err := b.Finish(shareA)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(keysA.Key, keysB.Key) {
		t.Fatal("the parties disagree")
	}
	phonePlain := `{"device":"Pixel 4 XL"}`
	serverPlain := `{"token":"0123456789abcdef","name":"desktop"}`
	phoneSealed, err := Seal(keysA.Key, LabelPhone, []byte(phonePlain), bytes.NewReader(bytes.Repeat([]byte{1}, 12)))
	if err != nil {
		t.Fatal(err)
	}
	serverSealed, err := Seal(keysB.Key, LabelServer, []byte(serverPlain), bytes.NewReader(bytes.Repeat([]byte{2}, 12)))
	if err != nil {
		t.Fatal(err)
	}
	return exchangeFixture{
		Phrase: strings.Join(phrase, " "), W: hex.EncodeToString(w),
		X: hex.EncodeToString(x), Y: hex.EncodeToString(y),
		ShareA: hex.EncodeToString(shareA), ShareB: hex.EncodeToString(shareB),
		Key:      hex.EncodeToString(keysA.Key),
		ConfirmA: hex.EncodeToString(keysA.Confirmation), ConfirmB: hex.EncodeToString(keysB.Confirmation),
		PhonePlaintext: phonePlain, PhoneSealed: hex.EncodeToString(phoneSealed),
		ServerPlaintext: serverPlain, ServerSealed: hex.EncodeToString(serverSealed),
	}
}

func TestExchangeFixtureIsCurrentAndSharedWithTheApp(t *testing.T) {
	want, err := json.MarshalIndent(buildFixture(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	path := filepath.Join("testdata", "exchange.json")
	appCopy := filepath.Join("..", "..", "..", "mobile", "app", "src", "test", "resources", "pairing-exchange.json")
	if *updateFixture {
		for _, p := range []string{path, appCopy} {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, want, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, p := range []string{path, appCopy} {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%v (run go test -update)", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is out of date (run go test -update)", p)
		}
	}
}
