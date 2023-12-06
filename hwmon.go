package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// pollHwmon tries to take advantage of a userspace notification feature of hwmon, but
// so far I've been unable to get it to work any better than naive polling.
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

// pollHwmon2 is a naive polling implementation.
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
