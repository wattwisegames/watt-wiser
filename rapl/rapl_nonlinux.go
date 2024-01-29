//go:build !linux && !windows

package rapl

import "git.sr.ht/~whereswaldon/watt-wiser/sensors"

func FindRAPL() ([]sensors.Sensor, error) {
	return nil, nil
}
