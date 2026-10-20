package main

import (
	"fmt"
	"time"

	"github.com/parsiya/f108-pro/pkg/aula"
)

// WatchCmd syncs the LCD clock whenever the keyboard connects or switches
// between USB and 2.4G. Switching connection modes resets the LCD to the
// settings menu; the official software restores it the same way.
type WatchCmd struct {
	Interval time.Duration `default:"15s" help:"How often to check the connection."`
}

// Run executes the watch command.
func (c *WatchCmd) Run() error {
	fmt.Printf("Watching for keyboard connections every %s (Ctrl+C to stop)\n", c.Interval)
	prev := ""
	for {
		mode := connectionMode()
		if mode != "" && mode != prev {
			fmt.Printf("%s: keyboard connected over %s\n", time.Now().Format(time.TimeOnly), mode)
			syncClockWithRetry()
		}
		prev = mode
		time.Sleep(c.Interval)
	}
}

// connectionMode returns "USB", "2.4G", or "" if the keyboard is not
// reachable. Over USB nothing is sent; over the dongle the battery query
// tells whether the keyboard is on and in 2.4G mode (the dongle itself is
// always present).
func connectionMode() string {
	dev, err := aula.Open()
	if err != nil {
		return ""
	}
	defer dev.Close()

	if !dev.Wireless() {
		return "USB"
	}
	if _, err := dev.Battery(); err != nil {
		return ""
	}
	return "2.4G"
}

// syncClockWithRetry syncs the clock, retrying because the keyboard may not
// respond right after a mode switch.
func syncClockWithRetry() {
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		var dev *aula.Device
		if dev, err = aula.Open(); err == nil {
			err = dev.SyncClock(time.Now())
			dev.Close()
		}
		if err == nil {
			fmt.Printf("%s: clock synced\n", time.Now().Format(time.TimeOnly))
			return
		}
		time.Sleep(time.Second)
	}
	fmt.Printf("%s: clock sync failed: %v\n", time.Now().Format(time.TimeOnly), err)
}
