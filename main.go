package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type watchFile struct {
	path       string
	deviceName string
	file       *os.File
	lastValue  int64
}

func pollRAPL() {
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

func pollHwmon() {
	const targetPath = "/sys/class/hwmon/hwmon7/temp1_input"
	f, err := os.Open(targetPath)
	if err != nil {
		log.Printf("failed opening: %v", err)
		return
	}
	defer f.Close()

	for {
		n, err := unix.Poll([]unix.PollFd{
			{
				Fd:      int32(f.Fd()),
				Events:  unix.POLLPRI | unix.POLLERR,
				Revents: 0,
			},
		}, 100)
		if err != nil {
			var errno syscall.Errno
			if errors.As(err, &errno) {
				if errno == syscall.EINTR {
					continue
				}
			}
			log.Printf("error polling: %T %#+v", err, err)
			return
		}
		err = f.Close()
		if err != nil {
			log.Printf("error closing: %v", err)
			return
		}
		f, err = os.Open(targetPath)
		if err != nil {
			log.Printf("error reopening: %v", err)
			return
		}
		var buf [256]byte
		n, err = f.Read(buf[:])
		if err != nil {
			log.Printf("error reading: %v", err)
			return
		}
		fmt.Printf("%s", buf[:n])
	}
}

func pollHwmon2() {
	const targetPath = "/sys/class/hwmon/hwmon2/in0_input"
	f, err := os.Open(targetPath)
	if err != nil {
		log.Printf("failed opening: %v", err)
		return
	}
	defer f.Close()

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	fmt.Print("timestamp_ns, ")
	var buf [256]byte
	for t := range ticker.C {
		fmt.Printf("%d, ", t.UnixNano())
		f.Seek(0, io.SeekStart)
		n, err := f.Read(buf[:])
		if err != nil {
			log.Printf("failed reading %s: %v", targetPath, err)
			continue
		}
		if n > 0 && buf[n-1] == 10 {
			n--
		}
		asInt, err := strconv.ParseInt(string(buf[:n]), 10, 64)
		if err != nil {
			log.Printf("failed parsing %s's value %s: %v", targetPath, string(buf[:n]), err)
			continue
		}
		fmt.Printf("%d, ", asInt)
		fmt.Println()
	}
}

func main() {
	relevantSubfeatures, err := FindSubfeatures()
	if err != nil {
		log.Fatal(err)
	}
	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for _, chip := range relevantSubfeatures {
				v, err := chip.Read()
				if err != nil {
					log.Fatalf("failed reading value: %v", err)
					return
				}
				fmt.Printf("%s %s %f\n", chip.Parent.Parent.Name, chip.Name, v)
			}
		}
	}
}
