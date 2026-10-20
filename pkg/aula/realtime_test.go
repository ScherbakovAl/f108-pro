package aula

import (
	"bytes"
	"testing"
)

// fakeUSB records feature reports and answers GET_REPORT from replies, then
// with zeros.
type fakeUSB struct {
	set     [][reportSize]byte
	gets    int
	replies [][reportSize]byte
}

func (f *fakeUSB) SetFeatureReport(data [reportSize]byte) error {
	f.set = append(f.set, data)
	return nil
}

func (f *fakeUSB) GetFeatureReport() ([reportSize]byte, error) {
	f.gets++
	if len(f.replies) == 0 {
		return [reportSize]byte{}, nil
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

func (f *fakeUSB) InitLCD() error                 { return nil }
func (f *fakeUSB) WriteLCDPage(data []byte) error { return nil }
func (f *fakeUSB) ReadLCDAck() ([]byte, error)    { return nil, nil }
func (f *fakeUSB) Close()                         {}

func TestRealtimeFRow(t *testing.T) {
	f := &fakeUSB{}
	d := &Device{transport: f}
	var keys []KeyColor
	for i := uint8(KeyF1); i <= KeyF12; i++ {
		keys = append(keys, KeyColor{LightIndex: i, G: i})
	}
	if err := d.SetRealtime(keys); err != nil {
		t.Fatal(err)
	}

	// Header announcing one data report, the report, apply.
	if len(f.set) != 3 || f.gets != 2 {
		t.Fatalf("sent %d reports with %d reads, want 3 with 2", len(f.set), f.gets)
	}
	if h := f.set[0]; h[0] != 0x04 || h[1] != 0x20 || h[8] != 1 {
		t.Errorf("header % x", h[:9])
	}
	data := f.set[1]
	for j := 0; j < 12; j++ {
		want := []byte{byte(KeyF1 + j), 0, byte(KeyF1 + j), 0}
		if got := data[j*4 : j*4+4]; !bytes.Equal(got, want) {
			t.Errorf("entry %d = % x, want % x", j, got, want)
		}
	}
	for j := 12; j < 16; j++ {
		if got := data[j*4 : j*4+4]; !bytes.Equal(got, []byte{KeyF12, 0, KeyF12, 0}) {
			t.Errorf("padding entry %d = % x, want a copy of F12", j, got)
		}
	}
	if a := f.set[2]; a[0] != 0x04 || a[1] != 0x02 {
		t.Errorf("apply % x", a[:2])
	}
}

func TestRealtimeReportCount(t *testing.T) {
	f := &fakeUSB{}
	d := &Device{transport: f}
	if err := d.SetRealtime(make([]KeyColor, 17)); err != nil {
		t.Fatal(err)
	}
	if f.set[0][8] != 2 || len(f.set) != 4 {
		t.Errorf("17 keys: header count %d, %d reports; want 2 and 4", f.set[0][8], len(f.set))
	}
}

func TestRealtimeDisplayedColors(t *testing.T) {
	var data [reportSize]byte
	for j := 0; j < 16; j++ {
		copy(data[j*4:], []byte{byte(j), byte(j), 0, 0xFF})
	}
	// Acknowledgment, data, apply acknowledgment.
	f := &fakeUSB{replies: [][reportSize]byte{{}, data}}
	d := &Device{transport: f}
	keys, err := d.DisplayedColors(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 16 || keys[KeyF1] != (KeyColor{LightIndex: KeyF1, R: KeyF1, B: 0xFF}) {
		t.Errorf("keys %+v", keys)
	}
	if c := f.set[0]; c[0] != 0x04 || c[1] != 0xF5 || c[8] != 1 {
		t.Errorf("command % x", c[:9])
	}
	if a := f.set[len(f.set)-1]; a[0] != 0x04 || a[1] != 0x02 {
		t.Errorf("readback not closed with 04 02: % x", a[:2])
	}
}
