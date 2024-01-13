//go:build !linux && !windows

package nvml

import "fmt"

func platformInit() error {
	return fmt.Errorf("unsupported platform for nvml")
}
