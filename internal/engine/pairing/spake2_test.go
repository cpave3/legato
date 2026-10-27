package pairing

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(strings.Join(strings.Fields(s), ""), "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The vectors of RFC 9382 Appendix B (SPAKE2-P256-SHA256-HKDF-HMAC, no AAD).
var rfcVectors = []struct {
	a, b                        string
	w, x, pA, y, pB, ke, cA, cB string
}{
	{
		a: "server", b: "client",
		w:  "2ee57912099d31560b3a44b1184b9b4866e904c49d12ac5042c97dca461b1a5f",
		x:  "43dd0fd7215bdcb482879fca3220c6a968e66d70b1356cac18bb26c84a78d729",
		pA: "04a56fa807caaa53a4d28dbb9853b9815c61a411118a6fe516a8798434751470f9010153ac33d0d5f2047ffdb1a3e42c9b4e6be662766e1eeb4116988ede5f912c",
		y:  "dcb60106f276b02606d8ef0a328c02e4b629f84f89786af5befb0bc75b6e66be",
		pB: "0406557e482bd03097ad0cbaa5df82115460d951e3451962f1eaf4367a420676d09857ccbc522686c83d1852abfa8ed6e4a1155cf8f1543ceca528afb591a1e0b7",
		ke: "0e0672dc86f8e45565d338b0540abe69",
		cA: "58ad4aa88e0b60d5061eb6b5dd93e80d9c4f00d127c65b3b35b1b5281fee38f0",
		cB: "d3e2e547f1ae04f2dbdbf0fc4b79f8ecff2dff314b5d32fe9fcef2fb26dc459b",
	},
	{
		a: "", b: "client",
		w:  "0548d8729f730589e579b0475a582c1608138ddf7054b73b5381c7e883e2efae",
		x:  "403abbe3b1b4b9ba17e3032849759d723939a27a27b9d921c500edde18ed654b",
		pA: "04a897b769e681c62ac1c2357319a3d363f610839c4477720d24cbe32f5fd85f44fb92ba966578c1b712be6962498834078262caa5b441ecfa9d4a9485720e918a",
		y:  "903023b6598908936ea7c929bd761af6039577a9c3f9581064187c3049d87065",
		pB: "04e0f816fd1c35e22065d5556215c097e799390d16661c386e0ecc84593974a61b881a8c82327687d0501862970c64565560cb5671f696048050ca66ca5f8cc7fc",
		ke: "642f05c473c2cd79909f9a841e2f30a7",
		cA: "47d29e6666af1b7dd450d571233085d7a9866e4d49d2645e2df975489521232b",
		cB: "3313c5cefc361d27fb16847a91c2a73b766ffa90a4839122a9b70a2f6bd1d6df",
	},
}

func TestSPAKE2MatchesRFC9382Vectors(t *testing.T) {
	for _, v := range rfcVectors {
		cfg := Config{IdentityA: []byte(v.a), IdentityB: []byte(v.b)}
		w := unhex(t, v.w)

		a, shareA, err := Start(PartyA, cfg, w, bytes.NewReader(unhex(t, v.x)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(shareA, unhex(t, v.pA)) {
			t.Fatalf("A=%q: pA = %x", v.a, shareA)
		}
		b, shareB, err := Start(PartyB, cfg, w, bytes.NewReader(unhex(t, v.y)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(shareB, unhex(t, v.pB)) {
			t.Fatalf("A=%q: pB = %x", v.a, shareB)
		}

		keysA, err := a.Finish(shareB)
		if err != nil {
			t.Fatal(err)
		}
		keysB, err := b.Finish(shareA)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(keysA.Key, unhex(t, v.ke)) || !bytes.Equal(keysB.Key, unhex(t, v.ke)) {
			t.Fatalf("A=%q: Ke = %x / %x", v.a, keysA.Key, keysB.Key)
		}
		if !bytes.Equal(keysA.Confirmation, unhex(t, v.cA)) {
			t.Fatalf("A=%q: cA = %x", v.a, keysA.Confirmation)
		}
		if !bytes.Equal(keysB.Confirmation, unhex(t, v.cB)) {
			t.Fatalf("A=%q: cB = %x", v.a, keysB.Confirmation)
		}
		if !keysA.Confirms(keysB.Confirmation) || !keysB.Confirms(keysA.Confirmation) {
			t.Fatalf("A=%q: the parties do not accept each other's confirmation", v.a)
		}
	}
}

func TestSPAKE2WithDifferentPasswordsDoesNotConfirm(t *testing.T) {
	cfg := Config{IdentityA: []byte("a"), IdentityB: []byte("b"), AAD: []byte("test")}
	a, shareA, err := Start(PartyA, cfg, PhraseScalar([]string{"acid", "acorn", "acre", "acts"}), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, shareB, err := Start(PartyB, cfg, PhraseScalar([]string{"acid", "acorn", "acre", "afar"}), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keysA, _ := a.Finish(shareB)
	keysB, _ := b.Finish(shareA)
	if keysB.Confirms(keysA.Confirmation) || keysA.Confirms(keysB.Confirmation) {
		t.Fatal("different phrases confirmed each other")
	}
}

func TestSPAKE2RefusesAShareThatIsNotAPoint(t *testing.T) {
	a, _, err := Start(PartyA, Config{}, PhraseScalar([]string{"acid"}), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Finish([]byte{4, 1, 2, 3}); err == nil {
		t.Fatal("a garbage share was accepted")
	}
}
