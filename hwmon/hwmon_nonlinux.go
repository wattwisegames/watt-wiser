//go:build !linux

package hwmon

import "git.sr.ht/~whereswaldon/watt-wiser/sensors"

func FindEnergySensors() ([]sensors.Sensor, error) {
	return nil, nil
}
