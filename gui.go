package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/parsiya/f108-pro/pkg/aula"
)

//go:embed gui/index.html
var guiFS embed.FS

// GUICmd serves a local web GUI that drives the keyboard.
type GUICmd struct {
	Addr      string `flag:"" default:"127.0.0.1:8108" help:"Address to listen on."`
	NoBrowser bool   `flag:"" help:"Do not open the browser automatically."`
}

// Run executes the gui command.
func (c *GUICmd) Run() error {
	ln, err := net.Listen("tcp", c.Addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	fmt.Printf("GUI: %s (Ctrl+C to stop)\n", url)

	if !c.NoBrowser {
		openBrowser(url)
	}
	return http.Serve(ln, newGUIHandler())
}

// openBrowser opens url in the default browser, ignoring failures.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// deviceMu serializes USB access across concurrent HTTP requests.
var deviceMu sync.Mutex

// withDevice opens the keyboard, runs fn, and closes it.
func withDevice(fn func(*aula.Device) error) error {
	deviceMu.Lock()
	defer deviceMu.Unlock()

	dev, err := aula.Open()
	if err != nil {
		return err
	}
	defer dev.Close()
	return fn(dev)
}

func newGUIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, guiFS, "gui/index.html")
	})
	mux.HandleFunc("GET /api/meta", handleMeta)
	mux.HandleFunc("POST /api/light", handleLight)
	mux.HandleFunc("POST /api/off", handleOff)
	mux.HandleFunc("POST /api/clock", handleClock)
	mux.HandleFunc("POST /api/perkey", handlePerkey)
	mux.HandleFunc("POST /api/remap", handleRemap)
	mux.HandleFunc("GET /api/presets", handleGetPresets)
	mux.HandleFunc("PUT /api/presets", handlePutPresets)
	return mux
}

// presetsPath returns the location of the presets file.
func presetsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "f108-pro", "presets.json"), nil
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// decodeJSON parses the request body into v.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// handleMeta returns static tables the UI needs to render its controls.
func handleMeta(w http.ResponseWriter, r *http.Request) {
	type mode struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	modes := make([]mode, 0, 20)
	for i := aula.LightingMode(0); i <= 19; i++ {
		modes = append(modes, mode{int(i), aula.ModeNames[i]})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modes":     modes,
		"keyRows":   keyRows,
		"media":     mapKeys(aula.ConsumerNameToCode),
		"mouse":     mapKeys(aula.MouseNameToParams),
		"modifiers": []string{"ctrl", "shift", "alt", "win", "rctrl", "rshift", "ralt", "rwin"},
	})
}

func handleLight(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode       int      `json:"mode"`
		Brightness uint8    `json:"brightness"`
		Speed      uint8    `json:"speed"`
		Color      [3]uint8 `json:"color"`
		Colorful   bool     `json:"colorful"`
		Direction  uint8    `json:"direction"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Direction > 3 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("direction %d out of range (0-3)", req.Direction))
		return
	}
	cfg := aula.LightingConfig{
		Mode:       aula.LightingMode(req.Mode),
		R:          req.Color[0],
		G:          req.Color[1],
		B:          req.Color[2],
		Brightness: req.Brightness,
		Speed:      req.Speed,
		Direction:  req.Direction,
		Colorful:   req.Colorful,
	}
	err := withDevice(func(d *aula.Device) error { return d.SetLighting(cfg) })
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, fmt.Sprintf("Mode %s applied.", aula.ModeNames[cfg.Mode]))
}

func handleOff(w http.ResponseWriter, r *http.Request) {
	err := withDevice(func(d *aula.Device) error {
		return d.SetLighting(aula.LightingConfig{Mode: aula.ModeOff})
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, "Backlight off.")
}

func handleClock(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	err := withDevice(func(d *aula.Device) error { return d.SyncClock(now) })
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, "Clock synced to "+now.Format("2006-01-02 15:04:05"))
}

func handlePerkey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		All        [3]uint8            `json:"all"`
		Brightness uint8               `json:"brightness"`
		Keys       map[string][3]uint8 `json:"keys"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	for name := range req.Keys {
		if _, ok := aula.KeyNameToIndex[name]; !ok {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown key '%s'", name))
			return
		}
	}
	keys := buildKeysFromLayout(req.All, req.Keys)
	if len(keys) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no keys specified"))
		return
	}
	err := withDevice(func(d *aula.Device) error { return d.SetPerKeyRGB(keys, req.Brightness) })
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, fmt.Sprintf("%d key(s) set.", len(keys)))
}

func handleRemap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FN    bool `json:"fn"`
		Reset bool `json:"reset"`
		Pairs []struct {
			Src string `json:"src"`
			Dst string `json:"dst"`
		} `json:"pairs"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	layer := "normal"
	if req.FN {
		layer = "FN"
	}

	if req.Reset {
		err := withDevice(func(d *aula.Device) error {
			if req.FN {
				return d.ResetFnKeyRemap()
			}
			return d.ResetKeyRemap()
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeOK(w, "All "+layer+" layer remaps cleared.")
		return
	}

	if len(req.Pairs) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no remap pairs specified"))
		return
	}
	var remaps []aula.KeyRemap
	for _, p := range req.Pairs {
		src := strings.ToLower(p.Src)
		srcIdx, ok := aula.KeyNameToIndex[src]
		if !ok {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown source key '%s'", p.Src))
			return
		}
		remap, err := parseRemapTarget(srcIdx, strings.ToLower(p.Dst))
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("target '%s': %w", p.Dst, err))
			return
		}
		remaps = append(remaps, remap)
	}

	err := withDevice(func(d *aula.Device) error {
		if req.FN {
			return d.SetFnKeyRemap(remaps)
		}
		return d.SetKeyRemap(remaps)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, fmt.Sprintf("%d remap(s) sent to %s layer.", len(remaps), layer))
}

// handleGetPresets returns the stored presets, or an empty list if none.
// The preset structure is owned by the UI; the server stores it verbatim.
func handleGetPresets(w http.ResponseWriter, r *http.Request) {
	path, err := presetsPath()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = []byte("[]")
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// handlePutPresets replaces the stored presets with the request body.
func handlePutPresets(w http.ResponseWriter, r *http.Request) {
	var presets []json.RawMessage
	if err := decodeJSON(r, &presets); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	path, err := presetsPath()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, "Presets saved.")
}
