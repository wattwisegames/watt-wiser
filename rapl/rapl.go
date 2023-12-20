package rapl

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"git.sr.ht/~whereswaldon/energy/sensors"
)

type watchFile struct {
	path       string
	deviceName string
	file       *os.File
	lastValue  int64
}

func (w *watchFile) Name() string {
	return w.deviceName
}

func (w *watchFile) Unit() sensors.Unit {
	return sensors.Joules
}

func (w *watchFile) Read() (float64, error) {
	var buf [256]byte
	w.file.Seek(0, io.SeekStart)
	n, err := w.file.Read(buf[:])
	if err != nil {
		return 0, fmt.Errorf("failed reading %s: %w", w.path, err)
	}
	if n > 0 && buf[n-1] == 10 {
		n--
	}
	asInt, err := strconv.ParseInt(string(buf[:n]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed parsing %s (%s): %w", w.path, string(buf[:n]), err)
	}
	increment := asInt - w.lastValue
	w.lastValue = asInt
	return float64(increment) * sensors.MicroToUnprefixed, nil
}

func FindRAPL() ([]*watchFile, error) {
	watchFiles := []*watchFile{}
	if err := filepath.WalkDir(
		"/sys/devices/virtual/powercap/intel-rapl",
		func(path string, d fs.DirEntry, err error) error {
			if d.Name() != "energy_uj" {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				log.Printf("failed opening file %q: %v", path, err)
				return nil
			}
			name, err := os.ReadFile(filepath.Join(filepath.Dir(path), "name"))
			if err != nil {
				log.Printf("failed resolving name for %q: %v", path, err)
			}
			w := &watchFile{
				path:       path,
				deviceName: strings.TrimSpace(string(name)),
				file:       file,
			}
			watchFiles = append(watchFiles, w)
			return nil
		},
	); err != nil {
		return nil, fmt.Errorf("failed traversing RAPL: %w", err)
	}
	return watchFiles, nil
}
