package aula

import (
	"bytes"
	"testing"
	"time"
)

// fakeDongle records sent reports and acknowledges each by echoing it.
// A battery query (20 01) is answered with the battery level in byte 3.
type fakeDongle struct {
	sent    [][wirelessReportSize]byte
	battery byte
}

func (f *fakeDongle) WriteReport(data [wirelessReportSize]byte) error {
	f.sent = append(f.sent, data)
	return nil
}

func (f *fakeDongle) ReadReport() ([wirelessReportSize]byte, error) {
	ack := f.sent[len(f.sent)-1]
	if ack[0] == 0x20 {
		ack[3] = f.battery
	}
	return ack, nil
}

func (f *fakeDongle) Close() {}

func checkChecksum(t *testing.T, r [wirelessReportSize]byte) {
	t.Helper()
	var sum byte
	for _, b := range r[:wirelessReportSize-1] {
		sum += b
	}
	if r[wirelessReportSize-1] != sum {
		t.Errorf("checksum %#x, want %#x in % x", r[wirelessReportSize-1], sum, r)
	}
}

func TestWirelessClock(t *testing.T) {
	f := &fakeDongle{}
	d := &Device{wireless: f}
	// 2026-10-20 19:45:30, Tuesday.
	if err := d.SyncClock(time.Date(2026, 10, 20, 19, 45, 30, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %d reports, want 1", len(f.sent))
	}
	want := []byte{0x0C, 0x10, 0, 0, 0x01, 0x5A, 26, 10, 20, 19, 45, 30, 0, 2, 0, 0, 0, 0xAA, 0x55}
	if got := f.sent[0][:len(want)]; !bytes.Equal(got, want) {
		t.Errorf("clock report\n got % x\nwant % x", got, want)
	}
	checkChecksum(t, f.sent[0])
}

func TestWirelessPerKey(t *testing.T) {
	f := &fakeDongle{}
	d := &Device{wireless: f}
	keys := []KeyColor{{LightIndex: KeyEsc, R: 1, G: 2, B: 3}, {LightIndex: KeyNumDot, R: 4, G: 5, B: 6}}
	if err := d.SetPerKeyRGB(keys, 4); err != nil {
		t.Fatal(err)
	}

	// Mode 0x80 with brightness, then 21 chunks.
	if len(f.sent) != 22 {
		t.Fatalf("sent %d reports, want 22", len(f.sent))
	}
	mode := f.sent[0]
	if mode[0] != 0x05 || mode[1] != 0x10 || mode[3] != 0x80 || mode[12] != 4 {
		t.Errorf("mode report % x", mode)
	}

	var buf []byte
	for i, r := range f.sent[1:] {
		checkChecksum(t, r)
		wantLen := byte(wirelessChunkSize)
		if i == 20 {
			wantLen = 0x10
		}
		if r[0] != 0x14 || r[1] != wantLen || r[2] != byte(i) {
			t.Errorf("chunk %d header % x", i, r[:3])
		}
		buf = append(buf, r[3:3+r[1]]...)
	}
	if len(buf) != perKeyRGBSize {
		t.Fatalf("reassembled %d bytes, want %d", len(buf), perKeyRGBSize)
	}
	if !bytes.Equal(buf[KeyEsc*4:KeyEsc*4+4], []byte{KeyEsc, 1, 2, 3}) ||
		!bytes.Equal(buf[KeyNumDot*4:KeyNumDot*4+4], []byte{KeyNumDot, 4, 5, 6}) {
		t.Error("key colors not at index*4")
	}
	if buf[perKeyRGBSize-2] != 0xAA || buf[perKeyRGBSize-1] != 0x55 {
		t.Errorf("trailer % x", buf[perKeyRGBSize-2:])
	}
}

func TestWirelessRemap(t *testing.T) {
	for _, tc := range []struct {
		fn  bool
		cmd byte
	}{{false, 0x10}, {true, 0x12}} {
		f := &fakeDongle{}
		d := &Device{wireless: f}
		r := NewKeySwap(KeyCapsLock, 0xE0) // CapsLock -> Left Ctrl.
		if err := d.sendRemapTable([]KeyRemap{r}, tc.fn); err != nil {
			t.Fatal(err)
		}
		if len(f.sent) != 21 {
			t.Fatalf("fn=%v: sent %d reports, want 21", tc.fn, len(f.sent))
		}
		var buf []byte
		for i, rep := range f.sent {
			checkChecksum(t, rep)
			if rep[0] != tc.cmd || rep[2] != byte(i) {
				t.Errorf("fn=%v chunk %d header % x", tc.fn, i, rep[:3])
			}
			buf = append(buf, rep[3:3+rep[1]]...)
		}
		if !bytes.Equal(buf, remapBuffer([]KeyRemap{r})) {
			t.Errorf("fn=%v: reassembled buffer differs from remapBuffer", tc.fn)
		}
	}
}

func TestWirelessLCD(t *testing.T) {
	f := &fakeDongle{}
	d := &Device{wireless: f}
	// One frame, padded to 4096 bytes like mkimage output.
	buf := make([]byte, 65536)
	buf[0] = 1
	if err := d.UploadLCDImage(buf, 1, nil); err != nil {
		t.Fatal(err)
	}

	const size = lcdHeaderSize + lcdFrameSize // 65056 bytes, not padded.
	const nChunks = (size + wirelessChunkSize - 1) / wirelessChunkSize
	if len(f.sent) != 1+nChunks {
		t.Fatalf("sent %d reports, want %d", len(f.sent), 1+nChunks)
	}
	hdr := f.sent[0]
	if !bytes.Equal(hdr[:7], []byte{0x7F, 0x03, 0, 1, nChunks & 0xFF, nChunks >> 8, 0}) {
		t.Errorf("header % x", hdr[:7])
	}
	last := f.sent[nChunks]
	i := nChunks - 1
	if last[0] != 0x80 || last[1] != byte(i) || last[2] != byte(i>>8) {
		t.Errorf("last chunk header % x, want index %d", last[:3], i)
	}
	tail := size - i*wirelessChunkSize
	if last[3+tail] != 0xFF || last[3+wirelessChunkSize-1] != 0xFF {
		t.Errorf("last chunk not padded with 0xFF: % x", last[:])
	}
	checkChecksum(t, last)
}

func TestWirelessBattery(t *testing.T) {
	d := &Device{wireless: &fakeDongle{battery: 87}}
	level, err := d.Battery()
	if err != nil || level != 87 {
		t.Errorf("Battery() = %d, %v; want 87", level, err)
	}
}
