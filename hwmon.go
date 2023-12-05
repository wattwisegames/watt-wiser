package main

import (
	"fmt"
	"log"
	"strings"
	"unsafe"
)

/*
#cgo LDFLAGS: -lsensors
#include <stdlib.h>
#include <sensors/sensors.h>
*/
import "C"

type Subfeature struct {
	Name    string
	Number  int
	Type    SubfeatureType
	Mapping int
	Flags   Flags
	Parent  Feature
}

func (s Subfeature) Read() (float64, error) {
	var value C.double
	rc := C.sensors_get_value(s.Parent.Parent.CChip, C.int(s.Number), &value)
	if rc < 0 {
		return 0, fmt.Errorf("failed reading subfeature value: %d", rc)
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

func FindSubfeatures() ([]Subfeature, error) {
	rc := C.sensors_init(nil)
	if rc != 0 {
		log.Fatalf("failed initializing sensors: %d", rc)
	}

	relevantSubfeatures := []Subfeature{}

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
			//			fmt.Printf("\t%#+v\n", currentFeature)
			var subfeatureIterState C.int
			for {
				subfeature := C.sensors_get_all_subfeatures(chip, feature, &subfeatureIterState)
				if subfeature == nil {
					break
				}
				currentSubfeature := Subfeature{
					Parent:  currentFeature,
					Name:    C.GoString(subfeature.name),
					Number:  int(subfeature.number),
					Type:    SubfeatureType(subfeature._type),
					Mapping: int(subfeature.mapping),
					Flags:   Flags(subfeature.flags),
				}
				relevantSubfeatures = append(relevantSubfeatures, currentSubfeature)
			}
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
