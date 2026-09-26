package telegram

import (
	"encoding/binary"
	"testing"
)

func TestParseMP4BoxHeader(t *testing.T) {
	normal := make([]byte, 8)
	binary.BigEndian.PutUint32(normal[:4], 24)
	copy(normal[4:], "ftyp")
	header, err := parseMP4BoxHeader(normal, 100)
	if err != nil || header.kind != "ftyp" || header.size != 24 || header.headerSize != 8 {
		t.Fatalf("unexpected normal header: %#v %v", header, err)
	}

	extended := make([]byte, 16)
	binary.BigEndian.PutUint32(extended[:4], 1)
	copy(extended[4:], "mdat")
	binary.BigEndian.PutUint64(extended[8:], 1<<32)
	header, err = parseMP4BoxHeader(extended, 1<<33)
	if err != nil || header.kind != "mdat" || header.size != 1<<32 || header.headerSize != 16 {
		t.Fatalf("unexpected extended header: %#v %v", header, err)
	}
}

func TestParseMP4BoxHeaderRejectsInvalidSize(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[:4], 4)
	copy(data[4:], "moov")
	if _, err := parseMP4BoxHeader(data, 100); err == nil {
		t.Fatal("invalid MP4 box size was accepted")
	}
}
