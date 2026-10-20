package aula

import (
	"errors"
	"fmt"
	"time"
)

const (
	// 2.4G TYPE-A dongle. It spoofs Apple's Aluminium Keyboard VID/PID.
	DongleVendorID    = 0x05AC
	DongleProductID   = 0x024F
	DongleInterfaceID = 3 // Raw HID channel (usage page 0xFF60).

	// Output/input report size on the dongle interface (no report ID).
	wirelessReportSize = 32

	// Per-key data is sent in 28-byte chunks (command 14).
	wirelessChunkSize = 0x1C

	// The official software waits up to 20 reports for an acknowledgment.
	wirelessAckTries = 20
)

// ErrWirelessUnsupported is returned for operations that only work over USB.
var ErrWirelessUnsupported = errors.New("not supported over the 2.4G dongle, connect the keyboard with a USB cable")

// wirelessTransport is the raw HID channel of the 2.4G dongle.
type wirelessTransport interface {
	// WriteReport sends a 32-byte output report.
	WriteReport(data [wirelessReportSize]byte) error

	// ReadReport reads a 32-byte input report with a timeout.
	ReadReport() ([wirelessReportSize]byte, error)

	Close()
}

// sendWireless fills in the checksum (last byte = sum of all bytes), sends
// the report, and waits for the keyboard to echo its first three bytes.
// Returns the acknowledgment. Mirrors FUN_0044f4e0 in the official software.
//
// A keyboard that has been idle can miss the first report, so it is resent
// once without an acknowledgment. All 2.4G commands are idempotent.
func (d *Device) sendWireless(report [wirelessReportSize]byte) ([wirelessReportSize]byte, error) {
	var sum byte
	for _, b := range report[:wirelessReportSize-1] {
		sum += b
	}
	report[wirelessReportSize-1] = sum

	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if err = d.wireless.WriteReport(report); err != nil {
			return report, err
		}
		var ack [wirelessReportSize]byte
		if ack, err = d.waitWirelessAck(report); err == nil {
			return ack, nil
		}
	}
	return report, err
}

// waitWirelessAck reads reports until one echoes the command's first three
// bytes.
func (d *Device) waitWirelessAck(report [wirelessReportSize]byte) ([wirelessReportSize]byte, error) {
	for i := 0; i < wirelessAckTries; i++ {
		ack, err := d.wireless.ReadReport()
		if err != nil {
			return ack, fmt.Errorf("no acknowledgment (is the keyboard on and in 2.4G mode?): %w", err)
		}
		if ack[0] == report[0] && ack[1] == report[1] && ack[2] == report[2] {
			return ack, nil
		}
	}
	return report, fmt.Errorf("no matching acknowledgment for command % x", report[:3])
}

// sendWirelessChunks sends buf as 28-byte chunks (cmd <len> <index> <data>)
// with a 2 ms pause, as used for per-key colors and key remaps.
func (d *Device) sendWirelessChunks(cmd byte, buf []byte) error {
	nChunks := (len(buf) + wirelessChunkSize - 1) / wirelessChunkSize
	for i := 0; i < nChunks; i++ {
		chunk := buf[i*wirelessChunkSize : min((i+1)*wirelessChunkSize, len(buf))]

		var report [wirelessReportSize]byte
		report[0] = cmd
		report[1] = byte(len(chunk))
		report[2] = byte(i)
		copy(report[3:], chunk)
		if _, err := d.sendWireless(report); err != nil {
			return fmt.Errorf("chunk %d/%d: %w", i+1, nChunks, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	return nil
}

// setLightingWireless sends the 2.4G all-in-one lighting packet (05 10).
// Layout: 05 10 00, then the same fields as the USB data packet.
func (d *Device) setLightingWireless(cfg LightingConfig) error {
	var report [wirelessReportSize]byte
	report[0] = 0x05
	report[1] = 0x10
	report[3] = byte(cfg.Mode)

	if cfg.Mode != 0 {
		report[4] = cfg.R
		report[5] = cfg.G
		report[6] = cfg.B

		colorful := uint8(0)
		if cfg.Colorful {
			colorful = 1
		}
		report[11] = colorful
		report[12] = cfg.Brightness
		report[13] = cfg.Speed
		report[14] = cfg.Direction
	}

	// Trailer as written by the official software (word 0x55AA, little-endian).
	report[17] = 0xAA
	report[18] = 0x55

	_, err := d.sendWireless(report)
	return err
}

// syncClockWireless sends the 2.4G clock packet (0C 10). Same fields as the
// USB clock data packet, shifted by 3 bytes. Mirrors FUN_00423c40.
func (d *Device) syncClockWireless(t time.Time) error {
	var report [wirelessReportSize]byte
	report[0] = 0x0C
	report[1] = 0x10
	report[4] = 0x01 // Profile 1.
	report[5] = 0x5A // Magic marker.
	report[6] = byte(t.Year() % 2000)
	report[7] = byte(t.Month())
	report[8] = byte(t.Day())
	report[9] = byte(t.Hour())
	report[10] = byte(t.Minute())
	report[11] = byte(t.Second())
	report[13] = byte(t.Weekday())
	report[17] = 0xAA
	report[18] = 0x55

	_, err := d.sendWireless(report)
	return err
}

// setPerKeyRGBWireless switches to the per-key mode (0x80) with the given
// brightness, then sends the 576-byte color buffer as 14-chunks.
// Mirrors FUN_0044bd80.
func (d *Device) setPerKeyRGBWireless(buf []byte, brightness uint8) error {
	if err := d.setLightingWireless(LightingConfig{Mode: 0x80, Brightness: brightness}); err != nil {
		return fmt.Errorf("per-key mode: %w", err)
	}
	return d.sendWirelessChunks(0x14, buf)
}

// uploadLCDWireless sends an LCD image over the dongle. Unlike USB, the
// buffer is not padded to 4096 bytes: only header + frames are sent, as
// 28-byte chunks with a 24-bit index. Mirrors FUN_004233f0.
func (d *Device) uploadLCDWireless(buf []byte, imageNumber uint8, progress func(sent, total int)) error {
	size := lcdHeaderSize + int(buf[0])*lcdFrameSize
	if buf[0] == 0 || size > len(buf) {
		return fmt.Errorf("invalid frame count %d for a %d-byte buffer", buf[0], len(buf))
	}
	data := buf[:size]
	nChunks := (size + wirelessChunkSize - 1) / wirelessChunkSize

	// Header: 7F 03 00 <image> <chunk count, 24-bit LE>.
	var hdr [wirelessReportSize]byte
	hdr[0] = 0x7F
	hdr[1] = 0x03
	hdr[3] = imageNumber
	hdr[4] = byte(nChunks)
	hdr[5] = byte(nChunks >> 8)
	hdr[6] = byte(nChunks >> 16)
	if _, err := d.sendWireless(hdr); err != nil {
		return fmt.Errorf("image header: %w", err)
	}

	for i := 0; i < nChunks; i++ {
		var report [wirelessReportSize]byte
		report[0] = 0x80 | byte(i>>16)
		report[1] = byte(i)
		report[2] = byte(i >> 8)
		// The last chunk is padded with 0xFF, like the official buffer.
		chunk := data[i*wirelessChunkSize : min((i+1)*wirelessChunkSize, size)]
		for j := copy(report[3:], chunk); j < wirelessChunkSize; j++ {
			report[3+j] = 0xFF
		}
		if _, err := d.sendWireless(report); err != nil {
			return fmt.Errorf("chunk %d/%d: %w", i+1, nChunks, err)
		}
		time.Sleep(2 * time.Millisecond)
		if progress != nil {
			progress(i+1, nChunks)
		}
	}
	return nil
}

// Battery returns the battery level in percent (20 01 query, level in byte 3
// of the reply). Only available over the dongle. Mirrors FUN_004357d0.
func (d *Device) Battery() (int, error) {
	if d.wireless == nil {
		return 0, errors.New("battery level is only available over the 2.4G dongle")
	}
	var report [wirelessReportSize]byte
	report[0] = 0x20
	report[1] = 0x01
	ack, err := d.sendWireless(report)
	if err != nil {
		return 0, err
	}
	if ack[3] == 0 {
		return 0, errors.New("keyboard did not report a battery level")
	}
	return min(int(ack[3]), 100), nil
}
