//go:build windows

package aula

import "errors"

func openWirelessTransport() (wirelessTransport, error) {
	return nil, errors.New("2.4G dongle is not supported on Windows yet")
}
