package main

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type watchFile struct {
	path       string
	deviceName string
	file       *os.File
	lastValue  int64
}

func main() {
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
			watchFiles = append(watchFiles, &watchFile{
				path:       path,
				deviceName: strings.TrimSpace(string(name)),
				file:       file,
			})
			return nil
		},
	); err != nil {
		log.Printf("failed traversing RAPL hierarchy: %v", err)
	}

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	fmt.Print("timestamp_ns, ")
	for _, watch := range watchFiles {
		fmt.Printf("%s, ", watch.deviceName)
	}
	fmt.Println()

	var buf [256]byte
	for t := range ticker.C {
		fmt.Printf("%d, ", t.UnixNano())
		for _, watch := range watchFiles {
			watch.file.Seek(0, io.SeekStart)
			n, err := watch.file.Read(buf[:])
			if err != nil {
				log.Printf("failed reading %s: %v", watch.path, err)
				continue
			}
			if n > 0 && buf[n-1] == 10 {
				n--
			}
			asInt, err := strconv.ParseInt(string(buf[:n]), 10, 64)
			if err != nil {
				log.Printf("failed parsing %s's value %s: %v", watch.path, string(buf[:n]), err)
				continue
			}
			increment := asInt - watch.lastValue
			watch.lastValue = asInt
			fmt.Printf("%d, ", increment)
		}
		fmt.Println()
	}
}
