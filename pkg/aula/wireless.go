package aula

import (
	"errors"
	"fmt"
)

const (
	// 2.4G TYPE-A dongle. It spoofs Apple's Aluminium Keyboard VID/PID.
	DongleVendorID    = 0x05AC
	DongleProductID   = 0x024F
	DongleInterfaceID = 3 // Raw HID channel (usage page 0xFF60).

	// Output/input report size on the dongle interface (no report ID).
	wirelessReportSize = 32
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

	report[17] = 0x55
	report[18] = 0xAA

	if err := d.wireless.WriteReport(report); err != nil {
		return err
	}

	// The keyboard acknowledges by echoing the 05 10 command.
	ack, err := d.wireless.ReadReport()
	if err != nil {
		return fmt.Errorf("no acknowledgment (is the keyboard on and in 2.4G mode?): %w", err)
	}
	if ack[0] != 0x05 || ack[1] != 0x10 {
		return fmt.Errorf("unexpected acknowledgment % x", ack[:4])
	}
	return nil
}
