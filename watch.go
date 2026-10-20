package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/parsiya/f108-pro/pkg/aula"
)

// D-Bus name, object path and interface of the F-row color method.
const (
	dbusName = "io.github.scherbakoval.F108Pro"
	dbusPath = "/io/github/scherbakoval/F108Pro"
)

// fRowKeepAlive resends the F-row frame well within aula.RealtimeTimeout.
const fRowKeepAlive = 2500 * time.Millisecond

// WatchCmd syncs the LCD clock whenever the keyboard connects or switches
// between USB and 2.4G. Switching connection modes resets the LCD to the
// settings menu; the official software restores it the same way.
//
// It also colors F1-F12 on request over D-Bus (SetFRowColor), which the
// panel-layout-color GNOME extension calls on every input source switch.
type WatchCmd struct {
	Interval time.Duration `default:"15s" help:"How often to check the connection."`
}

// Run executes the watch command.
func (c *WatchCmd) Run() error {
	colors := make(chan string, 1)
	if err := exportFRowColor(colors); err != nil {
		fmt.Printf("F-row colors disabled: %v\n", err)
	} else {
		fmt.Printf("F-row colors: call %s.SetFRowColor on the session bus\n", dbusName)
	}
	fmt.Printf("Watching for keyboard connections every %s (Ctrl+C to stop)\n", c.Interval)

	var row fRow
	prev := ""
	check := func() {
		mode := connectionMode()
		if mode != "" && mode != prev {
			fmt.Printf("%s: keyboard connected over %s\n", time.Now().Format(time.TimeOnly), mode)
			syncClockWithRetry()
			row.reconnected()
		}
		prev = mode
	}
	check()

	poll := time.NewTicker(c.Interval)
	keepAlive := time.NewTicker(fRowKeepAlive)
	for {
		select {
		case <-poll.C:
			check()
		case s := <-colors:
			row.set(s)
		case <-keepAlive.C:
			row.keepAlive()
		}
	}
}

// fRow shows a color on F1-F12 with realtime frames. They live in the
// keyboard's RAM only, so nothing wears the flash, and the other keys keep
// the keyboard's own lighting.
type fRow struct {
	color   *[3]uint8       // Wanted color, nil for the keyboard's own.
	shown   bool            // An overlay frame is on the keyboard.
	base    []aula.KeyColor // The F-row as the keyboard drew it.
	last    time.Time       // When the last frame was sent.
	lastErr string          // Last logged error, to log each one once.
}

// set takes a CSS color from SetFRowColor, "" for none.
func (r *fRow) set(s string) {
	var color *[3]uint8
	if s != "" {
		c, err := parseCSSColor(s)
		if err != nil {
			r.logErr(err)
			return
		}
		color = &c
	}
	same := color == nil && r.color == nil ||
		color != nil && r.color != nil && *color == *r.color
	if same {
		return
	}
	r.color = color
	r.show()
}

// keepAlive resends the overlay before the keyboard drops it.
func (r *fRow) keepAlive() {
	if r.shown {
		r.show()
	}
}

// reconnected forgets the overlay after a power cycle or mode switch, which
// clears the keyboard's RAM, and shows it again.
func (r *fRow) reconnected() {
	r.shown = false
	r.base = nil
	r.last = time.Time{}
	r.show()
}

func (r *fRow) show() {
	if r.color == nil {
		if r.shown {
			// Restore the keyboard's own colors right away; it takes over
			// again when the frame times out.
			r.shown = false
			if r.base != nil {
				r.send(r.base)
			}
		}
		return
	}

	err := withKeyboard(func(dev *aula.Device) error {
		// Remember the keyboard's own F-row, unless a frame of ours may
		// still be on it.
		if !r.shown && time.Since(r.last) > aula.RealtimeTimeout {
			shown, err := dev.DisplayedColors(1)
			if err != nil {
				return err
			}
			r.base = fRowOnly(shown)
		}
		return dev.SetRealtime(fRowKeys(*r.color))
	})
	if err != nil {
		r.logErr(err)
		return
	}
	r.shown = true
	r.last = time.Now()
	r.lastErr = ""
}

func (r *fRow) send(keys []aula.KeyColor) {
	err := withKeyboard(func(dev *aula.Device) error {
		return dev.SetRealtime(keys)
	})
	if err != nil {
		r.logErr(err)
		return
	}
	r.last = time.Now()
}

func (r *fRow) logErr(err error) {
	if err.Error() != r.lastErr {
		r.lastErr = err.Error()
		fmt.Printf("%s: F-row color: %v\n", time.Now().Format(time.TimeOnly), err)
	}
}

// withKeyboard opens the keyboard, runs fn, and closes it.
func withKeyboard(fn func(*aula.Device) error) error {
	dev, err := aula.Open()
	if err != nil {
		return err
	}
	defer dev.Close()
	return fn(dev)
}

// fRowKeys returns F1-F12 in one color.
func fRowKeys(c [3]uint8) []aula.KeyColor {
	var keys []aula.KeyColor
	for i := uint8(aula.KeyF1); i <= aula.KeyF12; i++ {
		keys = append(keys, aula.KeyColor{LightIndex: i, R: c[0], G: c[1], B: c[2]})
	}
	return keys
}

// fRowOnly keeps the F1-F12 entries of keys.
func fRowOnly(keys []aula.KeyColor) []aula.KeyColor {
	var row []aula.KeyColor
	for _, k := range keys {
		if k.LightIndex >= aula.KeyF1 && k.LightIndex <= aula.KeyF12 {
			row = append(row, k)
		}
	}
	return row
}

// parseCSSColor parses the colors GTK color pickers produce: #rgb, #rrggbb,
// rgb(r,g,b) and rgba(r,g,b,a). Alpha is ignored.
//
// parseCSSColor("rgb(83,19,19)") // [83 19 19]
func parseCSSColor(s string) ([3]uint8, error) {
	var c [3]uint8
	s = strings.TrimSpace(strings.ToLower(s))

	if hex, ok := strings.CutPrefix(s, "#"); ok {
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil || len(hex) != 6 {
			return c, fmt.Errorf("bad color %q", s)
		}
		return [3]uint8{uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
	}

	args, ok := strings.CutPrefix(s, "rgba(")
	if !ok {
		args, ok = strings.CutPrefix(s, "rgb(")
	}
	args, closed := strings.CutSuffix(args, ")")
	parts := strings.Split(args, ",")
	if !ok || !closed || len(parts) < 3 {
		return c, fmt.Errorf("bad color %q", s)
	}
	for i := range c {
		v, err := strconv.ParseUint(strings.TrimSpace(parts[i]), 10, 8)
		if err != nil {
			return c, fmt.Errorf("bad color %q", s)
		}
		c[i] = uint8(v)
	}
	return c, nil
}

// fRowColorService is exported on the session bus.
type fRowColorService struct {
	colors chan string
}

// SetFRowColor shows a CSS color on F1-F12, or the keyboard's own colors
// for "". Only the latest call counts.
func (s fRowColorService) SetFRowColor(color string) *dbus.Error {
	for {
		select {
		case s.colors <- color:
			return nil
		default:
			select { // Drop the older color nobody has picked up yet.
			case <-s.colors:
			default:
			}
		}
	}
}

// exportFRowColor publishes SetFRowColor on the session bus.
func exportFRowColor(colors chan string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	if err := conn.Export(fRowColorService{colors}, dbusPath, dbusName); err != nil {
		return err
	}
	reply, err := conn.RequestName(dbusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return errors.New(dbusName + " is taken, is another watch running?")
	}
	return nil
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
