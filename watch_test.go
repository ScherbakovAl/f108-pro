package main

import (
	"testing"

	"github.com/parsiya/f108-pro/pkg/aula"
)

func TestParseCSSColor(t *testing.T) {
	good := map[string][3]uint8{
		"#8b1e1e":             {0x8b, 0x1e, 0x1e},
		"#ABC":                {0xaa, 0xbb, 0xcc},
		"rgb(83,19,19)":       {83, 19, 19},
		" rgb(3, 53, 74) ":    {3, 53, 74},
		"rgba(255,0,128,0.5)": {255, 0, 128},
		"RGBA(0, 255, 0, 1)":  {0, 255, 0},
	}
	for s, want := range good {
		got, err := parseCSSColor(s)
		if err != nil || got != want {
			t.Errorf("parseCSSColor(%q) = %v, %v; want %v", s, got, err, want)
		}
	}

	for _, s := range []string{"", "red", "#12345", "#gggggg", "rgb(1,2)", "rgb(1,2,300)", "rgb(1,2,3"} {
		if _, err := parseCSSColor(s); err == nil {
			t.Errorf("parseCSSColor(%q) succeeded, want an error", s)
		}
	}
}

func TestFRowKeys(t *testing.T) {
	keys := fRowKeys([3]uint8{1, 2, 3})
	if len(keys) != 12 || keys[0].LightIndex != 2 || keys[11].LightIndex != 13 {
		t.Fatalf("fRowKeys: %+v", keys)
	}
	if row := fRowOnly(append(keys, keysOf(1, 14, 50)...)); len(row) != 12 {
		t.Fatalf("fRowOnly kept %d keys, want 12", len(row))
	}
}

func keysOf(indices ...uint8) (keys []aula.KeyColor) {
	for _, i := range indices {
		keys = append(keys, aula.KeyColor{LightIndex: i})
	}
	return keys
}
