//go:build !windows

package aula

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// hidrawTransport talks to the dongle through /dev/hidrawN, so no kernel
// driver detach is needed.
type hidrawTransport struct {
	f *os.File
}

func openWirelessTransport() (wirelessTransport, error) {
	path, err := findDongleHidraw()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return &hidrawTransport{f: f}, nil
}

// findDongleHidraw returns the hidraw node for the dongle's interface 3.
func findDongleHidraw() (string, error) {
	hidID := fmt.Sprintf("HID_ID=0003:%08X:%08X", DongleVendorID, DongleProductID)
	intfSuffix := fmt.Sprintf(":1.%d", DongleInterfaceID)

	nodes, _ := filepath.Glob("/sys/class/hidraw/hidraw*")
	for _, node := range nodes {
		uevent, err := os.ReadFile(filepath.Join(node, "device", "uevent"))
		if err != nil || !strings.Contains(string(uevent), hidID) {
			continue
		}
		hidDev, err := filepath.EvalSymlinks(filepath.Join(node, "device"))
		if err != nil {
			continue
		}
		// The HID device's parent is the USB interface, e.g. 3-7:1.3.
		if strings.HasSuffix(filepath.Base(filepath.Dir(hidDev)), intfSuffix) {
			return "/dev/" + filepath.Base(node), nil
		}
	}
	return "", fmt.Errorf("2.4G dongle not found (VID=%04x PID=%04x)", DongleVendorID, DongleProductID)
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
