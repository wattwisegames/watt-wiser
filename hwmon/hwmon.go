//go:build linux

package hwmon

/*
#cgo LDFLAGS: -lsensors
#include <stdlib.h>
#include <sensors/sensors.h>
*/
import "C"
import (
	"fmt"
	"log"
	"strings"
	"unsafe"

	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
)

type SyntheticPower struct {
	Current sensors.Sensor
	Voltage sensors.Sensor
}

func (s SyntheticPower) Name() string {
	return fmt.Sprintf("synthesized power (%s x %s)", s.Current.Name(), s.Voltage.Name())
}

func (s SyntheticPower) Unit() sensors.Unit {
	return sensors.Watts
}

func (s SyntheticPower) Read() (float64, error) {
	current, err := s.Current.Read()
	if err != nil {
		return 0, fmt.Errorf("failed reading current: %w", err)
	}
	voltage, err := s.Voltage.Read()
	if err != nil {
		return 0, fmt.Errorf("failed reading voltage: %w", err)
	}
	return current * voltage, nil
}

type Subfeature struct {
	SubName string
	Number  int
	Type    SubfeatureType
	Mapping int
	Flags   Flags
	Parent  Feature
}

func (s Subfeature) Name() string {
	return s.Parent.Parent.Name + "#" + s.SubName
}

func (s Subfeature) Unit() sensors.Unit {
	switch s.Parent.Type {
	case C.SENSORS_FEATURE_IN:
		return sensors.Volts
	case C.SENSORS_FEATURE_POWER:
		return sensors.Watts
	case C.SENSORS_FEATURE_ENERGY:
		return sensors.Joules
	case C.SENSORS_FEATURE_CURR:
		return sensors.Amps
	default:
		return sensors.Unknown
	}
}

func (s Subfeature) Read() (float64, error) {
	var value C.double
	rc := C.sensors_get_value(s.Parent.Parent.CChip, C.int(s.Number), &value)
	if rc < 0 {
		return 0, fmt.Errorf("failed reading subfeature value: %d", rc)
	}
	ret := float64(value)
	switch s.Parent.Type {
	case C.SENSORS_FEATURE_IN:
		ret *= sensors.MicroToUnprefixed
	case C.SENSORS_FEATURE_POWER:
		ret *= sensors.MicroToUnprefixed
	case C.SENSORS_FEATURE_ENERGY:
		ret *= sensors.MicroToUnprefixed
	case C.SENSORS_FEATURE_CURR:
		ret *= sensors.MicroToUnprefixed
	}
	return float64(value), nil
}

type Feature struct {
	Name   string
	Label  string
	Number int
	Type   FeatureType
	Parent Chip
}

type Chip struct {
	Name  string
	CChip *C.sensors_chip_name
}

func FindEnergySensors() ([]sensors.Sensor, error) {
	rc := C.sensors_init(nil)
	if rc != 0 {
		log.Fatalf("failed initializing sensors: %d", rc)
	}

	relevantSubfeatures := []sensors.Sensor{}

	var chipIterState C.int
	for {
		chip := C.sensors_get_detected_chips(nil, &chipIterState)
		if chip == nil {
			break
		}
		var currentChip Chip
		var buf [256]C.char
		rc := C.sensors_snprintf_chip_name(&buf[0], C.ulong(len(buf)), chip)
		if rc >= 0 {
			currentChip.Name = C.GoString(&buf[0])
			currentChip.CChip = chip
			//			fmt.Printf("chip: %#+v\n", currentChip)
		} else {
			continue
		}

		hasCurrent := false
		var currentSensor Subfeature
		hasVoltage := false
		var voltageSensor Subfeature
		var featureIterState C.int
		for {
			feature := C.sensors_get_features(chip, &featureIterState)
			if feature == nil {
				break
			}
			switch feature._type {
			case C.SENSORS_FEATURE_IN:
			case C.SENSORS_FEATURE_POWER:
			case C.SENSORS_FEATURE_ENERGY:
			case C.SENSORS_FEATURE_CURR:
			default:
				continue
			}
			currentFeature := Feature{
				Parent: currentChip,
				Name:   C.GoString(feature.name),
				Number: int(feature.number),
				Type:   FeatureType(feature._type),
			}
			cLabel := C.sensors_get_label(chip, feature)
			if cLabel != nil {
				currentFeature.Label = C.GoString(cLabel)
				C.free(unsafe.Pointer(cLabel))
			}
			var subfeatureIterState C.int
			switch feature._type {
			case C.SENSORS_FEATURE_POWER, C.SENSORS_FEATURE_ENERGY:
				for {
					subfeature := C.sensors_get_all_subfeatures(chip, feature, &subfeatureIterState)
					if subfeature == nil {
						break
					}
					switch subfeature._type {
					case C.SENSORS_SUBFEATURE_POWER_INPUT:
					case C.SENSORS_SUBFEATURE_POWER_AVERAGE:
					case C.SENSORS_SUBFEATURE_POWER_AVERAGE_INTERVAL:
					case C.SENSORS_SUBFEATURE_ENERGY_INPUT:
					default:
						continue
					}
					currentSubfeature := Subfeature{
						Parent:  currentFeature,
						SubName: C.GoString(subfeature.name),
						Number:  int(subfeature.number),
						Type:    SubfeatureType(subfeature._type),
						Mapping: int(subfeature.mapping),
						Flags:   Flags(subfeature.flags),
					}
					relevantSubfeatures = append(relevantSubfeatures, currentSubfeature)
				}
			case C.SENSORS_FEATURE_IN, C.SENSORS_FEATURE_CURR:
				for {
					subfeature := C.sensors_get_all_subfeatures(chip, feature, &subfeatureIterState)
					if subfeature == nil {
						break
					}
					currentSubfeature := Subfeature{
						Parent:  currentFeature,
						SubName: C.GoString(subfeature.name),
						Number:  int(subfeature.number),
						Type:    SubfeatureType(subfeature._type),
						Mapping: int(subfeature.mapping),
						Flags:   Flags(subfeature.flags),
					}
					switch subfeature._type {
					case C.SENSORS_SUBFEATURE_CURR_INPUT:
						hasCurrent = true
						currentSensor = currentSubfeature
					case C.SENSORS_SUBFEATURE_IN_INPUT:
						hasVoltage = true
						voltageSensor = currentSubfeature
					default:
						continue
					}
				}
			}
		}
		if hasCurrent && hasVoltage {
			relevantSubfeatures = append(relevantSubfeatures, SyntheticPower{
				Current: currentSensor,
				Voltage: voltageSensor,
			})
		}
	}
	return relevantSubfeatures, nil
}

type FeatureType C.sensors_feature_type

func (f FeatureType) String() string {
	switch f {
	case C.SENSORS_FEATURE_IN:
		return "voltage"
	case C.SENSORS_FEATURE_FAN:
		return "fan speed"
	case C.SENSORS_FEATURE_TEMP:
		return "temperature"
	case C.SENSORS_FEATURE_POWER:
		return "power"
	case C.SENSORS_FEATURE_ENERGY:
		return "energy"
	case C.SENSORS_FEATURE_CURR:
		return "current"
	case C.SENSORS_FEATURE_HUMIDITY:
		return "humidity"
	case C.SENSORS_FEATURE_MAX_MAIN:
		return "max main"
	case C.SENSORS_FEATURE_VID:
		return "cpu core reference voltage"
	case C.SENSORS_FEATURE_INTRUSION:
		return "intrusion"
	case C.SENSORS_FEATURE_MAX_OTHER:
		return "max other"
	case C.SENSORS_FEATURE_BEEP_ENABLE:
		return "beep enable"
	case C.SENSORS_FEATURE_MAX:
		return "feature maximum"
	case C.SENSORS_FEATURE_UNKNOWN:
		return "feature unknown"
	default:
		return "unknown feature type"
	}
}

type SubfeatureType C.sensors_subfeature_type

func (s SubfeatureType) String() string {
	switch s {
	case C.SENSORS_SUBFEATURE_IN_INPUT:
		return "voltage"
	case C.SENSORS_SUBFEATURE_IN_MIN:
		return "minimum voltage"
	case C.SENSORS_SUBFEATURE_IN_MAX:
		return "maximum voltage"
	case C.SENSORS_SUBFEATURE_IN_LCRIT:
		return "critical minimum voltage"
	case C.SENSORS_SUBFEATURE_IN_CRIT:
		return "critical maximum voltage"
	case C.SENSORS_SUBFEATURE_IN_AVERAGE:
		return "average voltage"
	case C.SENSORS_SUBFEATURE_IN_LOWEST:
		return "historical minimum voltage"
	case C.SENSORS_SUBFEATURE_IN_HIGHEST:
		return "historical maximum voltage"
	case C.SENSORS_SUBFEATURE_IN_ALARM:
		return "voltage alarm "
	case C.SENSORS_SUBFEATURE_IN_MIN_ALARM:
		return "voltage minimum alarm"
	case C.SENSORS_SUBFEATURE_IN_MAX_ALARM:
		return "voltage maximum alarm"
	case C.SENSORS_SUBFEATURE_IN_BEEP:
		return "voltage beep"
	case C.SENSORS_SUBFEATURE_IN_LCRIT_ALARM:
		return "critical minimum voltage alarm"
	case C.SENSORS_SUBFEATURE_IN_CRIT_ALARM:
		return "critical maximum voltage alarm"

	case C.SENSORS_SUBFEATURE_FAN_INPUT:
		return "SENSORS_SUBFEATURE_FAN_INPUT"
	case C.SENSORS_SUBFEATURE_FAN_MIN:
		return "SENSORS_SUBFEATURE_FAN_MIN"
	case C.SENSORS_SUBFEATURE_FAN_MAX:
		return "SENSORS_SUBFEATURE_FAN_MAX"
	case C.SENSORS_SUBFEATURE_FAN_ALARM:
		return "SENSORS_SUBFEATURE_FAN_ALARM"
	case C.SENSORS_SUBFEATURE_FAN_FAULT:
		return "SENSORS_SUBFEATURE_FAN_FAULT"
	case C.SENSORS_SUBFEATURE_FAN_DIV:
		return "SENSORS_SUBFEATURE_FAN_DIV"
	case C.SENSORS_SUBFEATURE_FAN_BEEP:
		return "SENSORS_SUBFEATURE_FAN_BEEP"
	case C.SENSORS_SUBFEATURE_FAN_PULSES:
		return "SENSORS_SUBFEATURE_FAN_PULSES"
	case C.SENSORS_SUBFEATURE_FAN_MIN_ALARM:
		return "SENSORS_SUBFEATURE_FAN_MIN_ALARM"
	case C.SENSORS_SUBFEATURE_FAN_MAX_ALARM:
		return "SENSORS_SUBFEATURE_FAN_MAX_ALARM"
	case C.SENSORS_SUBFEATURE_TEMP_INPUT:
		return "SENSORS_SUBFEATURE_TEMP_INPUT"
	case C.SENSORS_SUBFEATURE_TEMP_MAX:
		return "SENSORS_SUBFEATURE_TEMP_MAX"
	case C.SENSORS_SUBFEATURE_TEMP_MAX_HYST:
		return "SENSORS_SUBFEATURE_TEMP_MAX_HYST"
	case C.SENSORS_SUBFEATURE_TEMP_MIN:
		return "SENSORS_SUBFEATURE_TEMP_MIN"
	case C.SENSORS_SUBFEATURE_TEMP_CRIT:
		return "SENSORS_SUBFEATURE_TEMP_CRIT"
	case C.SENSORS_SUBFEATURE_TEMP_CRIT_HYST:
		return "SENSORS_SUBFEATURE_TEMP_CRIT_HYST"
	case C.SENSORS_SUBFEATURE_TEMP_LCRIT:
		return "SENSORS_SUBFEATURE_TEMP_LCRIT"
	case C.SENSORS_SUBFEATURE_TEMP_EMERGENCY:
		return "SENSORS_SUBFEATURE_TEMP_EMERGENCY"
	case C.SENSORS_SUBFEATURE_TEMP_EMERGENCY_HYST:
		return "SENSORS_SUBFEATURE_TEMP_EMERGENCY_HYST"
	case C.SENSORS_SUBFEATURE_TEMP_LOWEST:
		return "SENSORS_SUBFEATURE_TEMP_LOWEST"
	case C.SENSORS_SUBFEATURE_TEMP_HIGHEST:
		return "SENSORS_SUBFEATURE_TEMP_HIGHEST"
	case C.SENSORS_SUBFEATURE_TEMP_MIN_HYST:
		return "SENSORS_SUBFEATURE_TEMP_MIN_HYST"
	case C.SENSORS_SUBFEATURE_TEMP_LCRIT_HYST:
		return "SENSORS_SUBFEATURE_TEMP_LCRIT_HYST"
	case C.SENSORS_SUBFEATURE_TEMP_ALARM:
		return "SENSORS_SUBFEATURE_TEMP_ALARM"
	case C.SENSORS_SUBFEATURE_TEMP_MAX_ALARM:
		return "SENSORS_SUBFEATURE_TEMP_MAX_ALARM"
	case C.SENSORS_SUBFEATURE_TEMP_MIN_ALARM:
		return "SENSORS_SUBFEATURE_TEMP_MIN_ALARM"
	case C.SENSORS_SUBFEATURE_TEMP_CRIT_ALARM:
		return "SENSORS_SUBFEATURE_TEMP_CRIT_ALARM"
	case C.SENSORS_SUBFEATURE_TEMP_FAULT:
		return "SENSORS_SUBFEATURE_TEMP_FAULT"
	case C.SENSORS_SUBFEATURE_TEMP_TYPE:
		return "SENSORS_SUBFEATURE_TEMP_TYPE"
	case C.SENSORS_SUBFEATURE_TEMP_OFFSET:
		return "SENSORS_SUBFEATURE_TEMP_OFFSET"
	case C.SENSORS_SUBFEATURE_TEMP_BEEP:
		return "SENSORS_SUBFEATURE_TEMP_BEEP"
	case C.SENSORS_SUBFEATURE_TEMP_EMERGENCY_ALARM:
		return "SENSORS_SUBFEATURE_TEMP_EMERGENCY_ALARM"
	case C.SENSORS_SUBFEATURE_TEMP_LCRIT_ALARM:
		return "SENSORS_SUBFEATURE_TEMP_LCRIT_ALARM"

	case C.SENSORS_SUBFEATURE_POWER_AVERAGE:
		return "power average"
	case C.SENSORS_SUBFEATURE_POWER_AVERAGE_HIGHEST:
		return "historical maximum power average"
	case C.SENSORS_SUBFEATURE_POWER_AVERAGE_LOWEST:
		return "historical minimum power average"
	case C.SENSORS_SUBFEATURE_POWER_INPUT:
		return "power input"
	case C.SENSORS_SUBFEATURE_POWER_INPUT_HIGHEST:
		return "historical maximum power input"
	case C.SENSORS_SUBFEATURE_POWER_INPUT_LOWEST:
		return "historical minimum power input"
	case C.SENSORS_SUBFEATURE_POWER_CAP:
		return "power cap"
	case C.SENSORS_SUBFEATURE_POWER_CAP_HYST:
		return "power cap hysteresis"
	case C.SENSORS_SUBFEATURE_POWER_MAX:
		return "maximum power"
	case C.SENSORS_SUBFEATURE_POWER_CRIT:
		return "critical maximum power"
	case C.SENSORS_SUBFEATURE_POWER_MIN:
		return "minimum power"
	case C.SENSORS_SUBFEATURE_POWER_LCRIT:
		return "critical minimum power"
	case C.SENSORS_SUBFEATURE_POWER_AVERAGE_INTERVAL:
		return "power average interval"
	case C.SENSORS_SUBFEATURE_POWER_ALARM:
		return "power alarm"
	case C.SENSORS_SUBFEATURE_POWER_CAP_ALARM:
		return "power cap alarm"
	case C.SENSORS_SUBFEATURE_POWER_MAX_ALARM:
		return "maximum power alarm"
	case C.SENSORS_SUBFEATURE_POWER_CRIT_ALARM:
		return "critical maximum power alarm"
	case C.SENSORS_SUBFEATURE_POWER_MIN_ALARM:
		return "minimum power alarm"
	case C.SENSORS_SUBFEATURE_POWER_LCRIT_ALARM:
		return "critical minimum power alarm"
	case C.SENSORS_SUBFEATURE_ENERGY_INPUT:
		return "energy input"
	case C.SENSORS_SUBFEATURE_CURR_INPUT:
		return "current input"
	case C.SENSORS_SUBFEATURE_CURR_MIN:
		return "minimum current"
	case C.SENSORS_SUBFEATURE_CURR_MAX:
		return "maximum current"
	case C.SENSORS_SUBFEATURE_CURR_LCRIT:
		return "critical minimum current"
	case C.SENSORS_SUBFEATURE_CURR_CRIT:
		return "critical maximum current"
	case C.SENSORS_SUBFEATURE_CURR_AVERAGE:
		return "current average"
	case C.SENSORS_SUBFEATURE_CURR_LOWEST:
		return "historical minimum current"
	case C.SENSORS_SUBFEATURE_CURR_HIGHEST:
		return "historical maximum current"
	case C.SENSORS_SUBFEATURE_CURR_ALARM:
		return "current alarm"
	case C.SENSORS_SUBFEATURE_CURR_MIN_ALARM:
		return "minimum current alarm"
	case C.SENSORS_SUBFEATURE_CURR_MAX_ALARM:
		return "maximum current alarm"
	case C.SENSORS_SUBFEATURE_CURR_BEEP:
		return "current beep"
	case C.SENSORS_SUBFEATURE_CURR_LCRIT_ALARM:
		return "critical minimum current alarm"
	case C.SENSORS_SUBFEATURE_CURR_CRIT_ALARM:
		return "critical maximum current alarm"

	case C.SENSORS_SUBFEATURE_HUMIDITY_INPUT:
		return "SENSORS_SUBFEATURE_HUMIDITY_INPUT"

	case C.SENSORS_SUBFEATURE_VID:
		return "cpu core reference voltage"

	case C.SENSORS_SUBFEATURE_INTRUSION_ALARM:
		return "SENSORS_SUBFEATURE_INTRUSION_ALARM"
	case C.SENSORS_SUBFEATURE_INTRUSION_BEEP:
		return "SENSORS_SUBFEATURE_INTRUSION_BEEP"
	case C.SENSORS_SUBFEATURE_BEEP_ENABLE:
		return "SENSORS_SUBFEATURE_BEEP_ENABLE"
	case C.SENSORS_SUBFEATURE_UNKNOWN:
		return "SENSORS_SUBFEATURE_UNKNOWN"
	default:
		return "unknown subfeature type"
	}
}

type Flags uint

func (f Flags) String() string {
	var b strings.Builder
	if f&C.SENSORS_MODE_R != 0 {
		b.WriteString("R")
	}
	if f&C.SENSORS_MODE_W != 0 {
		if b.Len() > 0 {
			b.WriteString("|")
		}
		b.WriteString("W")
	}
	if f&C.SENSORS_COMPUTE_MAPPING != 0 {
		if b.Len() > 0 {
			b.WriteString("|")
		}
		b.WriteString("COMPUTE_MAPPING")
	}
	return b.String()
}

/*
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
*/
