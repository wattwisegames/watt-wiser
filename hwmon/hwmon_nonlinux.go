//go:build !linux

package hwmon

import "git.sr.ht/~whereswaldon/energy/sensors"

func FindEnergySensors() ([]sensors.Sensor, error) {
	return nil, nil
}
