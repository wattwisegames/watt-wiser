package nvml

import (
	"fmt"
	"log"
	"sync"

	"git.sr.ht/~whereswaldon/energy/sensors"
)

var (
	// once protects the initialization process.
	once sync.Once
	// initErr tracks whether the one-time initialization succeeded or failed.
	initErr error
	// The rest of these are wrapper funcs populated by initialization.
	nvmlInit                            func() error
	nvmlSystemGetNVMLVersion            func() (string, error)
	nvmlDeviceGetCount                  func() (uint64, error)
	nvmlDeviceGetHandleByIndex          func(i uint64) (uintptr, error)
	nvmlDeviceGetName                   func(device uintptr) (string, error)
	nvmlDeviceGetArchitecture           func(device uintptr) (nvmlDeviceArchitecture, error)
	nvmlDeviceGetTotalEnergyConsumption func(device uintptr) (uint64, error)
	nvmlDeviceGetPowerUsage             func(device uintptr) (uint32, error)
)

func load() error {
	once.Do(func() {
		initErr = platformInit()
	})
	return initErr
}

func FindGPUSensors() ([]sensors.Sensor, error) {
	if err := load(); err != nil {
		return nil, err
	}
	if err := nvmlInit(); err != nil {
		return nil, fmt.Errorf("failed initializing nvml: %w", err)
	}
	version, err := nvmlSystemGetNVMLVersion()
	if err != nil {
		return nil, fmt.Errorf("failed querying nvml version: %w", err)
	}
	log.Printf("Using NVML version %q", version)
	count, err := nvmlDeviceGetCount()
	if err != nil {
		return nil, fmt.Errorf("failed counting gpus: %w", err)
	}
	out := []sensors.Sensor{}
	for i := uint64(0); i < count; i++ {
		device, err := nvmlDeviceGetHandleByIndex(i)
		if err != nil {
			log.Printf("failed acquiring handle to NVIDIA GPU at index %d: %v", i, err)
			continue
		}
		name, err := nvmlDeviceGetName(device)
		if err != nil {
			log.Printf("failed loading NVIDIA GPU name at index %d: %v", i, err)
			continue
		}
		s := sensor{
			name:   name,
			device: device,
		}
		if nvmlDeviceGetTotalEnergyConsumption != nil {
			// If we can't query architecture, but we can check energy, just try reading
			// energy to check if it's supported.
			_, err = nvmlDeviceGetTotalEnergyConsumption(device)
			if err != nil {
				_, err = nvmlDeviceGetPowerUsage(device)
				if err != nil {
					// This device does not support power monitoring of any kind.
					log.Printf("discarding NVIDIA GPU %q because does not support power monitoring: %v", name, err)
					continue
				}
				s.unit = sensors.Watts
			} else {
				s.unit = sensors.Joules
			}
		} else {
			_, err = nvmlDeviceGetPowerUsage(device)
			if err != nil {
				// This device does not support power monitoring of any kind.
				log.Printf("discarding NVIDIA GPU %q because does not support power monitoring: %v", name, err)
				continue
			}
			s.unit = sensors.Watts
		}
		out = append(out, &s)
	}
	return out, nil
}

type sensor struct {
	name       string
	unit       sensors.Unit
	device     uintptr
	lastReadMJ uint64
}

func (s *sensor) Name() string {
	return s.name
}

func (s *sensor) Unit() sensors.Unit {
	return s.unit
}

func (s *sensor) Read() (float64, error) {
	if s.unit == sensors.Watts {
		mW, err := nvmlDeviceGetPowerUsage(s.device)
		if err != nil {
			return 0, err
		}
		return float64(mW) / 1_000, nil
	}
	mJ, err := nvmlDeviceGetTotalEnergyConsumption(s.device)
	if err != nil {
		return 0, err
	}
	mJ, s.lastReadMJ = mJ-s.lastReadMJ, mJ
	return float64(mJ) / 1_000, nil
}

var _ sensors.Sensor = (*sensor)(nil)

const (
	symbolNvmlInit_v2                         string = "nvmlInit_v2"
	symbolNvmlSystemGetNVMLVersion            string = "nvmlSystemGetNVMLVersion"
	symbolNvmlDeviceGetCount_v2               string = "nvmlDeviceGetCount_v2"
	symbolNvmlDeviceGetHandleByIndex_v2       string = "nvmlDeviceGetHandleByIndex_v2"
	symbolNvmlDeviceGetName                   string = "nvmlDeviceGetName"
	symbolNvmlDeviceGetTotalEnergyConsumption string = "nvmlDeviceGetTotalEnergyConsumption"
	symbolNvmlDeviceGetPowerUsage             string = "nvmlDeviceGetPowerUsage"
	symbolNvmlDeviceGetArchitecture           string = "nvmlDeviceGetArchitecture"
)

var (
	requiredSymbols = []string{
		symbolNvmlInit_v2,
		symbolNvmlSystemGetNVMLVersion,
		symbolNvmlDeviceGetCount_v2,
		symbolNvmlDeviceGetHandleByIndex_v2,
		symbolNvmlDeviceGetPowerUsage,
		symbolNvmlDeviceGetName,
	}
	optionalSymbols = []string{
		symbolNvmlDeviceGetTotalEnergyConsumption,
		symbolNvmlDeviceGetArchitecture,
	}
)

const (
	NVML_DEVICE_ARCH_KEPLER  = 2          // Devices based on the NVIDIA Kepler architecture
	NVML_DEVICE_ARCH_MAXWELL = 3          // Devices based on the NVIDIA Maxwell architecture
	NVML_DEVICE_ARCH_PASCAL  = 4          // Devices based on the NVIDIA Pascal architecture
	NVML_DEVICE_ARCH_VOLTA   = 5          // Devices based on the NVIDIA Volta architecture
	NVML_DEVICE_ARCH_TURING  = 6          // Devices based on the NVIDIA Turing architecture
	NVML_DEVICE_ARCH_AMPERE  = 7          // Devices based on the NVIDIA Ampere architecture
	NVML_DEVICE_ARCH_ADA     = 8          // Devices based on the NVIDIA Ada architecture
	NVML_DEVICE_ARCH_HOPPER  = 9          // Devices based on the NVIDIA Hopper architecture
	NVML_DEVICE_ARCH_UNKNOWN = 0xffffffff // Anything else, presumably something newer
)

type nvmlDeviceArchitecture uint32

type nvmlError uint32

const (
	NVML_SUCCESS                         nvmlError = 0   //!< The operation was successful
	NVML_ERROR_UNINITIALIZED             nvmlError = 1   //!< NVML was not first initialized with nvmlInit()
	NVML_ERROR_INVALID_ARGUMENT          nvmlError = 2   //!< A supplied argument is invalid
	NVML_ERROR_NOT_SUPPORTED             nvmlError = 3   //!< The requested operation is not available on target device
	NVML_ERROR_NO_PERMISSION             nvmlError = 4   //!< The current user does not have permission for operation
	NVML_ERROR_ALREADY_INITIALIZED       nvmlError = 5   //!< Deprecated: Multiple initializations are now allowed through ref counting
	NVML_ERROR_NOT_FOUND                 nvmlError = 6   //!< A query to find an object was unsuccessful
	NVML_ERROR_INSUFFICIENT_SIZE         nvmlError = 7   //!< An input argument is not large enough
	NVML_ERROR_INSUFFICIENT_POWER        nvmlError = 8   //!< A device's external power cables are not properly attached
	NVML_ERROR_DRIVER_NOT_LOADED         nvmlError = 9   //!< NVIDIA driver is not loaded
	NVML_ERROR_TIMEOUT                   nvmlError = 10  //!< User provided timeout passed
	NVML_ERROR_IRQ_ISSUE                 nvmlError = 11  //!< NVIDIA Kernel detected an interrupt issue with a GPU
	NVML_ERROR_LIBRARY_NOT_FOUND         nvmlError = 12  //!< NVML Shared Library couldn't be found or loaded
	NVML_ERROR_FUNCTION_NOT_FOUND        nvmlError = 13  //!< Local version of NVML doesn't implement this function
	NVML_ERROR_CORRUPTED_INFOROM         nvmlError = 14  //!< infoROM is corrupted
	NVML_ERROR_GPU_IS_LOST               nvmlError = 15  //!< The GPU has fallen off the bus or has otherwise become inaccessible
	NVML_ERROR_RESET_REQUIRED            nvmlError = 16  //!< The GPU requires a reset before it can be used again
	NVML_ERROR_OPERATING_SYSTEM          nvmlError = 17  //!< The GPU control device has been blocked by the operating system/cgroups
	NVML_ERROR_LIB_RM_VERSION_MISMATCH   nvmlError = 18  //!< RM detects a driver/library version mismatch
	NVML_ERROR_IN_USE                    nvmlError = 19  //!< An operation cannot be performed because the GPU is currently in use
	NVML_ERROR_MEMORY                    nvmlError = 20  //!< Insufficient memory
	NVML_ERROR_NO_DATA                   nvmlError = 21  //!< No data
	NVML_ERROR_VGPU_ECC_NOT_SUPPORTED    nvmlError = 22  //!< The requested vgpu operation is not available on target device becasue ECC is enabled
	NVML_ERROR_INSUFFICIENT_RESOURCES    nvmlError = 23  //!< Ran out of critical resources other than memory
	NVML_ERROR_FREQ_NOT_SUPPORTED        nvmlError = 24  //!< Ran out of critical resources other than memory
	NVML_ERROR_ARGUMENT_VERSION_MISMATCH nvmlError = 25  //!< The provided version is invalid/unsupported
	NVML_ERROR_DEPRECATED                nvmlError = 26  //!< The requested functionality has been deprecated
	NVML_ERROR_NOT_READY                 nvmlError = 27  //!< The system is not ready for the request
	NVML_ERROR_GPU_NOT_FOUND             nvmlError = 28  //!< No GPUs were found
	NVML_ERROR_UNKNOWN                   nvmlError = 999 //!< An internal driver error occurred

)

func (n nvmlError) Error() string {
	switch n {
	case NVML_SUCCESS:
		return "The operation was successful"
	case NVML_ERROR_UNINITIALIZED:
		return "NVML was not first initialized with nvmlInit()"
	case NVML_ERROR_INVALID_ARGUMENT:
		return "A supplied argument is invalid"
	case NVML_ERROR_NOT_SUPPORTED:
		return "The requested operation is not available on target device"
	case NVML_ERROR_NO_PERMISSION:
		return "The current user does not have permission for operation"
	case NVML_ERROR_ALREADY_INITIALIZED:
		return "Deprecated: Multiple initializations are now allowed through ref counting"
	case NVML_ERROR_NOT_FOUND:
		return "A query to find an object was unsuccessful"
	case NVML_ERROR_INSUFFICIENT_SIZE:
		return "An input argument is not large enough"
	case NVML_ERROR_INSUFFICIENT_POWER:
		return "A device's external power cables are not properly attached"
	case NVML_ERROR_DRIVER_NOT_LOADED:
		return "NVIDIA driver is not loaded"
	case NVML_ERROR_TIMEOUT:
		return "User provided timeout passed"
	case NVML_ERROR_IRQ_ISSUE:
		return "NVIDIA Kernel detected an interrupt issue with a GPU"
	case NVML_ERROR_LIBRARY_NOT_FOUND:
		return "NVML Shared Library couldn't be found or loaded"
	case NVML_ERROR_FUNCTION_NOT_FOUND:
		return "Local version of NVML doesn't implement this function"
	case NVML_ERROR_CORRUPTED_INFOROM:
		return "infoROM is corrupted"
	case NVML_ERROR_GPU_IS_LOST:
		return "The GPU has fallen off the bus or has otherwise become inaccessible"
	case NVML_ERROR_RESET_REQUIRED:
		return "The GPU requires a reset before it can be used again"
	case NVML_ERROR_OPERATING_SYSTEM:
		return "The GPU control device has been blocked by the operating system/cgroups"
	case NVML_ERROR_LIB_RM_VERSION_MISMATCH:
		return "RM detects a driver/library version mismatch"
	case NVML_ERROR_IN_USE:
		return "An operation cannot be performed because the GPU is currently in use"
	case NVML_ERROR_MEMORY:
		return "Insufficient memory"
	case NVML_ERROR_NO_DATA:
		return "No data"
	case NVML_ERROR_VGPU_ECC_NOT_SUPPORTED:
		return "The requested vgpu operation is not available on target device becasue ECC is enabled"
	case NVML_ERROR_INSUFFICIENT_RESOURCES:
		return "Ran out of critical resources other than memory"
	case NVML_ERROR_FREQ_NOT_SUPPORTED:
		return "Ran out of critical resources other than memory"
	case NVML_ERROR_ARGUMENT_VERSION_MISMATCH:
		return "The provided version is invalid/unsupported"
	case NVML_ERROR_DEPRECATED:
		return "The requested functionality has been deprecated"
	case NVML_ERROR_NOT_READY:
		return "The system is not ready for the request"
	case NVML_ERROR_GPU_NOT_FOUND:
		return "No GPUs were found"
	case NVML_ERROR_UNKNOWN:
		return "An internal driver error occurred"
	default:
		return "An unrecognized error occurred"
	}
}
