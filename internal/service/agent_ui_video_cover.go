package service

import (
	"encoding/binary"
	"io"
)

// An MP4 file is a tree of boxes: four bytes of length, four of a name, then
// the content, which for some boxes is more boxes. A cover picture is at
//
//	moov / udta / meta / ilst / covr / data
//
// where data holds eight bytes about the picture and then the picture.

// mp4CoverMaxBoxes bounds how many boxes mp4Cover looks at, so that a file
// made to waste time cannot.
const mp4CoverMaxBoxes = 4096

// mp4Cover returns the cover picture an MP4 file carries, or nil when it has
// none, is not an MP4, or is not well formed. It reads the boxes' headers
// and the picture, never the video itself.
func mp4Cover(file io.ReaderAt, size int64) []byte {
	budget := mp4CoverMaxBoxes
	start, end := int64(0), size
	for _, name := range []string{"moov", "udta", "meta", "ilst", "covr", "data"} {
		var found bool
		start, end, found = mp4FindBox(file, start, end, name, &budget)
		if !found {
			return nil
		}
		if name == "meta" {
			start = mp4MetaContent(file, start, end)
		}
	}
	// A data box starts with the kind of its value and a locale.
	start += 8
	if end-start <= 0 || end-start > AgentUIMaxImageBytes {
		return nil
	}
	picture := make([]byte, end-start)
	if _, err := file.ReadAt(picture, start); err != nil {
		return nil
	}
	return picture
}

// mp4FindBox looks among the boxes between start and end for the first one
// called name, and returns where its content starts and ends.
func mp4FindBox(file io.ReaderAt, start, end int64, name string, budget *int) (int64, int64, bool) {
	var header [16]byte
	for at := start; at+8 <= end; {
		if *budget--; *budget < 0 {
			return 0, 0, false
		}
		if _, err := file.ReadAt(header[:8], at); err != nil {
			return 0, 0, false
		}
		length, headerLength := int64(binary.BigEndian.Uint32(header[:4])), int64(8)
		switch length {
		case 0: // the box runs to the end of what holds it
			length = end - at
		case 1: // the real length follows the name, in eight bytes
			if _, err := file.ReadAt(header[8:16], at+8); err != nil {
				return 0, 0, false
			}
			length, headerLength = int64(binary.BigEndian.Uint64(header[8:16])), 16
		}
		if length < headerLength || at+length > end {
			return 0, 0, false
		}
		if string(header[4:8]) == name {
			return at + headerLength, at + length, true
		}
		at += length
	}
	return 0, 0, false
}

// mp4MetaContent returns where the boxes inside a meta box start. In an MP4
// file a meta box has four bytes of version and flags before them; in a
// QuickTime file it has none. The first box inside is a handler, hdlr,
// which tells the two apart.
func mp4MetaContent(file io.ReaderAt, start, end int64) int64 {
	var probe [8]byte
	if start+8 <= end {
		if _, err := file.ReadAt(probe[:], start); err == nil && string(probe[4:8]) == "hdlr" {
			return start
		}
	}
	return start + 4
}
