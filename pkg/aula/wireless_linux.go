//go:build !windows

package aula

import (
	"fmt"
	"os"
	"time"
)

// hidrawTransport talks to the dongle through /dev/hidrawN, so no kernel
// driver detach is needed.
type hidrawTransport struct {
	f *os.File
}

func openWirelessTransport() (wirelessTransport, error) {
	path, err := findHidraw(DongleVendorID, DongleProductID, DongleInterfaceID)
	if err != nil {
		return nil, fmt.Errorf("2.4G dongle: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return &hidrawTransport{f: f}, nil
}

func (t *hidrawTransport) WriteReport(data [wirelessReportSize]byte) error {
	// hidraw expects a leading report ID byte, 0 for unnumbered reports.
	buf := make([]byte, 1+wirelessReportSize)
	copy(buf[1:], data[:])
	if _, err := t.f.Write(buf); err != nil {
		return fmt.Errorf("hidraw write: %w", err)
	}
	return nil
}

func (t *hidrawTransport) ReadReport() ([wirelessReportSize]byte, error) {
	var report [wirelessReportSize]byte
	if err := t.f.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		return report, err
	}
	if _, err := t.f.Read(report[:]); err != nil {
		return report, fmt.Errorf("hidraw read: %w", err)
	}
	return report, nil
}

func (t *hidrawTransport) Close() {
	t.f.Close()
}
