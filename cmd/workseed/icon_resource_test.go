package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestWindowsResourceContainsFaviconImages(t *testing.T) {
	icon, err := os.ReadFile("../../web/public/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	resource, err := os.ReadFile("rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatal(err)
	}
	if len(icon) < 6 || binary.LittleEndian.Uint16(icon[2:4]) != 1 {
		t.Fatal("favicon.ico is not a valid icon file")
	}
	count := int(binary.LittleEndian.Uint16(icon[4:6]))
	if count == 0 || len(icon) < 6+count*16 {
		t.Fatalf("favicon.ico has an invalid image count: %d", count)
	}
	for index := range count {
		entry := icon[6+index*16 : 6+(index+1)*16]
		size := int(binary.LittleEndian.Uint32(entry[8:12]))
		offset := int(binary.LittleEndian.Uint32(entry[12:16]))
		if size <= 0 || offset < 0 || offset > len(icon)-size {
			t.Fatalf("favicon.ico image %d has invalid bounds", index)
		}
		if !bytes.Contains(resource, icon[offset:offset+size]) {
			t.Fatalf("Windows resource is missing favicon image %d", index)
		}
	}
}
