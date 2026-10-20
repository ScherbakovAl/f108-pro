package aula

import (
	"fmt"
	"time"
)

// RealtimeTimeout is how long the keyboard shows a realtime frame before it
// returns to its own lighting: the firmware arms a 50 x 100 ms timer on every
// 04 20 command. Resend frames well within it to keep them on.
const RealtimeTimeout = 5 * time.Second

// realtimeEntries is the number of [index R G B] entries per report.
const realtimeEntries = reportSize / 4

// SetRealtime shows colors on the given keys until RealtimeTimeout passes,
// leaving the other keys as the keyboard draws them. Nothing is saved to
// flash. While a frame is shown, animated effects are paused.
//
// Sequence: 04 20 (byte[8] = number of data reports) -> data -> 04 02.
// The count must match the reports sent: the firmware treats whatever comes
// next as LED data until it has them all.
//
// dev.SetRealtime([]aula.KeyColor{{LightIndex: aula.KeyF1, G: 255}})
func (d *Device) SetRealtime(keys []KeyColor) error {
	if d.wireless != nil {
		return ErrWirelessUnsupported
	}
	if len(keys) == 0 {
		return nil
	}
	nReports := (len(keys) + realtimeEntries - 1) / realtimeEntries

	// The GET_REPORT after the header is what makes the firmware accept data.
	header := make([]byte, 64)
	header[0] = 0x04
	header[1] = 0x20
	header[8] = byte(nReports)
	if err := d.sendCommand(header, true); err != nil {
		return fmt.Errorf("realtime header: %w", err)
	}

	for i := 0; i < nReports; i++ {
		// Unused entries repeat the last key: zeros would paint index 0 black.
		var report [reportSize]byte
		for j := 0; j < realtimeEntries; j++ {
			k := keys[min(i*realtimeEntries+j, len(keys)-1)]
			copy(report[j*4:], []byte{k.LightIndex, k.R, k.G, k.B})
		}
		if err := d.setFeatureReport(report); err != nil {
			return fmt.Errorf("realtime data %d/%d: %w", i+1, nReports, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	if err := d.applyTransaction(); err != nil {
		return fmt.Errorf("realtime apply: %w", err)
	}
	return nil
}

// DisplayedColors reads the colors the keyboard is showing right now for
// light indices 0 to 16*nReports-1. The values are the raw LED levels, so
// passing them to SetRealtime shows exactly the same colors.
//
// Sequence: 04 F5 (byte[8] = number of reports) -> one GET_REPORT per report
// of 16 [index R G B] entries -> 04 02. Until the 04 02, the firmware
// rejects any other command.
//
// dev.DisplayedColors(1) // Esc, F1-F12 and a few more.
func (d *Device) DisplayedColors(nReports int) ([]KeyColor, error) {
	if d.wireless != nil {
		return nil, ErrWirelessUnsupported
	}

	cmd := make([]byte, 64)
	cmd[0] = 0x04
	cmd[1] = 0xF5
	cmd[8] = byte(nReports)
	if err := d.sendCommand(cmd, true); err != nil {
		return nil, fmt.Errorf("color readback: %w", err)
	}

	var keys []KeyColor
	for i := 0; i < nReports; i++ {
		report, err := d.getFeatureReport()
		if err != nil {
			return nil, fmt.Errorf("color readback %d/%d: %w", i+1, nReports, err)
		}
		for j := 0; j < realtimeEntries; j++ {
			e := report[j*4 : j*4+4]
			keys = append(keys, KeyColor{LightIndex: e[0], R: e[1], G: e[2], B: e[3]})
		}
		time.Sleep(2 * time.Millisecond)
	}

	if err := d.applyTransaction(); err != nil {
		return nil, fmt.Errorf("color readback apply: %w", err)
	}
	return keys, nil
}
