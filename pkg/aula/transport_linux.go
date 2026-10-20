//go:build !windows

package aula

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// hidrawUSBTransport implements Transport using /dev/hidrawN nodes. Unlike
// libusb, hidraw does not detach the kernel driver, so the keyboard keeps
// typing while (and after) the tool talks to it.
type hidrawUSBTransport struct {
	cfg *os.File // Interface 3: feature reports.
	lcd *os.File // Interface 2: LCD data, opened lazily.
}

func openTransport() (Transport, error) {
	path, err := findHidraw(VendorID, ProductID, InterfaceID)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return &hidrawUSBTransport{cfg: f}, nil
}

// findHidraw returns the hidraw node for the given USB interface.
func findHidraw(vid, pid uint16, intf int) (string, error) {
	hidID := fmt.Sprintf("HID_ID=0003:%08X:%08X", vid, pid)
	intfSuffix := fmt.Sprintf(":1.%d", intf)

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
	return "", fmt.Errorf("device not found (VID=%04x PID=%04x interface %d)", vid, pid, intf)
}

// hidiocFeature builds HIDIOCSFEATURE (nr 6) / HIDIOCGFEATURE (nr 7) for a
// buffer of the given length: _IOC(_IOC_READ|_IOC_WRITE, 'H', nr, len).
func hidiocFeature(nr, length uintptr) uintptr {
	return 3<<30 | length<<16 | 'H'<<8 | nr
}

// featureIoctl runs a feature report ioctl. buf[0] is the report ID (0);
// the kernel strips it for devices without numbered reports.
func featureIoctl(f *os.File, nr uintptr, buf []byte) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		hidiocFeature(nr, uintptr(len(buf))), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

func (t *hidrawUSBTransport) SetFeatureReport(data [reportSize]byte) error {
	buf := make([]byte, 1+reportSize)
	copy(buf[1:], data[:])
	if err := featureIoctl(t.cfg, 6, buf); err != nil {
		return fmt.Errorf("SET_REPORT: %w", err)
	}
	return nil
}

func (t *hidrawUSBTransport) GetFeatureReport() ([reportSize]byte, error) {
	var report [reportSize]byte
	buf := make([]byte, 1+reportSize)
	if err := featureIoctl(t.cfg, 7, buf); err != nil {
		return report, fmt.Errorf("GET_REPORT: %w", err)
	}
	copy(report[:], buf[1:])
	return report, nil
}

func (t *hidrawUSBTransport) InitLCD() error {
	if t.lcd != nil {
		return nil
	}
	path, err := findHidraw(VendorID, ProductID, lcdInterfaceID)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	t.lcd = f
	return nil
}

// WriteLCDPage sends a 4096-byte output report. The kernel uses the
// interrupt OUT endpoint, as the official software does with WriteFile.
func (t *hidrawUSBTransport) WriteLCDPage(data []byte) error {
	buf := make([]byte, 1+len(data))
	copy(buf[1:], data)
	_, err := t.lcd.Write(buf)
	return err
}

func (t *hidrawUSBTransport) ReadLCDAck() ([]byte, error) {
	buf := make([]byte, 64)
	if err := t.lcd.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		return buf, err
	}
	_, err := t.lcd.Read(buf)
	return buf, err
}

func (t *hidrawUSBTransport) Close() {
	if t.lcd != nil {
		t.lcd.Close()
	}
	t.cfg.Close()
}
