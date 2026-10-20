//go:build windows

package aula

// lockDevice is a no-op on Windows, where the watch service does not run.
func lockDevice() (unlock func(), err error) {
	return func() {}, nil
}
