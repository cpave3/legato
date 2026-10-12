package service

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// mp4Box builds one box: its length, its name, and its content.
func mp4Box(name string, content ...[]byte) []byte {
	body := bytes.Join(content, nil)
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out[:4], uint32(8+len(body)))
	copy(out[4:8], name)
	return append(out, body...)
}

// mp4WithCover builds the boxes of an MP4 that carries a cover picture, as
// ffmpeg writes them.
func mp4WithCover(picture []byte) []byte {
	data := mp4Box("data", []byte{0, 0, 0, 14, 0, 0, 0, 0}, picture)
	ilst := mp4Box("ilst", mp4Box("\xa9too", mp4Box("data", []byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte("Lavf"))), mp4Box("covr", data))
	meta := mp4Box("meta", []byte{0, 0, 0, 0}, mp4Box("hdlr", make([]byte, 25)), ilst)
	moov := mp4Box("moov", mp4Box("mvhd", make([]byte, 100)), mp4Box("trak", make([]byte, 40)), mp4Box("udta", meta))
	return bytes.Join([][]byte{mp4Box("ftyp", []byte("isom\x00\x00\x02\x00isommp41")), moov, mp4Box("mdat", make([]byte, 64))}, nil)
}

func TestMP4CoverFindsThePictureAVideoCarries(t *testing.T) {
	picture := []byte("\x89PNG\r\n\x1a\nthe poster")
	file := mp4WithCover(picture)
	if got := mp4Cover(bytes.NewReader(file), int64(len(file))); !bytes.Equal(got, picture) {
		t.Fatalf("cover = %q, want %q", got, picture)
	}
}

func TestMP4CoverReadsAMetaBoxWithNoVersion(t *testing.T) {
	// QuickTime writes a meta box whose boxes start at once.
	picture := []byte("\xff\xd8\xff\xe0jpeg")
	ilst := mp4Box("ilst", mp4Box("covr", mp4Box("data", []byte{0, 0, 0, 13, 0, 0, 0, 0}, picture)))
	file := mp4Box("moov", mp4Box("udta", mp4Box("meta", mp4Box("hdlr", make([]byte, 25)), ilst)))
	if got := mp4Cover(bytes.NewReader(file), int64(len(file))); !bytes.Equal(got, picture) {
		t.Fatalf("cover = %q, want %q", got, picture)
	}
}

func TestMP4CoverReadsABoxWithALongLength(t *testing.T) {
	picture := []byte("\x89PNG\r\n\x1a\nlong")
	inner := mp4Box("udta", mp4Box("meta", []byte{0, 0, 0, 0}, mp4Box("hdlr", make([]byte, 25)),
		mp4Box("ilst", mp4Box("covr", mp4Box("data", make([]byte, 8), picture)))))
	// moov with its length written in eight bytes.
	moov := make([]byte, 16)
	binary.BigEndian.PutUint32(moov[:4], 1)
	copy(moov[4:8], "moov")
	binary.BigEndian.PutUint64(moov[8:16], uint64(16+len(inner)))
	file := append(moov, inner...)
	if got := mp4Cover(bytes.NewReader(file), int64(len(file))); !bytes.Equal(got, picture) {
		t.Fatalf("cover = %q, want %q", got, picture)
	}
}

func TestMP4CoverIsNilForAFileWithNoneOrOneThatIsBroken(t *testing.T) {
	whole := mp4WithCover([]byte("\x89PNG\r\n\x1a\npicture"))
	lying := append([]byte(nil), whole...)
	// The first box claims to be longer than the file.
	binary.BigEndian.PutUint32(lying[:4], uint32(len(lying)+100))
	tooShort := mp4Box("moov", mp4Box("udta", mp4Box("meta", []byte{0, 0, 0, 0}, mp4Box("ilst", mp4Box("covr", mp4Box("data", []byte{0, 0}))))))
	zeroLength := []byte{0, 0, 0, 4, 'm', 'o', 'o', 'v'} // a box shorter than its own header
	for name, file := range map[string][]byte{
		"no cover":                      mp4Box("moov", mp4Box("mvhd", make([]byte, 100))),
		"not an MP4":                    []byte("<html>not a video</html>"),
		"nothing":                       nil,
		"cut off":                       whole[:len(whole)/2],
		"a box past the end":            lying,
		"a data box with no picture":    tooShort,
		"a box shorter than its header": zeroLength,
	} {
		if got := mp4Cover(bytes.NewReader(file), int64(len(file))); got != nil {
			t.Errorf("%s: cover = %q, want none", name, got)
		}
	}
}

func TestMP4CoverGivesUpOnAFileOfEndlessBoxes(t *testing.T) {
	// Far more boxes than any real file has before its moov box.
	file := bytes.Repeat(mp4Box("free"), mp4CoverMaxBoxes+10)
	file = append(file, mp4WithCover([]byte("\x89PNG\r\n\x1a\nlate"))...)
	if got := mp4Cover(bytes.NewReader(file), int64(len(file))); got != nil {
		t.Errorf("cover = %q; want the search to stop first", got)
	}
}
