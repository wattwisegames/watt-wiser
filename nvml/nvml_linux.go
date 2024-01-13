//go:build linux

package nvml

/*
#cgo LDFLAGS: -ldl
#cgo pkg-config: nvidia-ml
#include <nvml.h>
#include <dlfcn.h>

typedef nvmlReturn_t (*nvmlInit_v2_type)();

nvmlReturn_t call_nvmlInit_v2(void *func) {
	return ((nvmlInit_v2_type) func)();
}

typedef nvmlReturn_t (*nvmlSystemGetNVMLVersion_type)(char *version, unsigned int length);

nvmlReturn_t call_nvmlSystemGetNVMLVersion(void *func, char *version, unsigned int length) {
	return ((nvmlSystemGetNVMLVersion_type) func)(version, length);
}

typedef nvmlReturn_t (*nvmlDeviceGetCount_v2_type)(unsigned int *deviceCount);

nvmlReturn_t call_nvmlDeviceGetCount_v2(void *func, unsigned int *deviceCount) {
	return ((nvmlDeviceGetCount_v2_type) func)(deviceCount);
}

typedef nvmlReturn_t (*nvmlDeviceGetHandleByIndex_v2_type)(unsigned int index, nvmlDevice_t *device);

nvmlReturn_t call_nvmlDeviceGetHandleByIndex_v2(void *func, unsigned int index, nvmlDevice_t *device) {
	return ((nvmlDeviceGetHandleByIndex_v2_type) func)(index, device);
}

typedef nvmlReturn_t (*nvmlDeviceGetTotalEnergyConsumption_type)(nvmlDevice_t device, unsigned long long *energy);

nvmlReturn_t call_nvmlDeviceGetTotalEnergyConsumption(void *func, nvmlDevice_t device, unsigned long long *energy) {
	return ((nvmlDeviceGetTotalEnergyConsumption_type) func)(device, energy);
}

typedef nvmlReturn_t (*nvmlDeviceGetPowerUsage_type)(nvmlDevice_t device, unsigned int *power);

nvmlReturn_t call_nvmlDeviceGetPowerUsage(void *func, nvmlDevice_t device, unsigned int *power) {
	return ((nvmlDeviceGetPowerUsage_type) func)(device, power);
}

typedef nvmlReturn_t (*nvmlDeviceGetName_type)(nvmlDevice_t device, char *vgpuTypeName, unsigned int *size);

nvmlReturn_t call_nvmlDeviceGetName(void *func, nvmlDevice_t device, char *vgpuTypeName, unsigned int *size) {
	return ((nvmlDeviceGetName_type) func)(device, vgpuTypeName, size);
}

typedef nvmlReturn_t (*nvmlDeviceGetArchitecture_type)(nvmlDevice_t device, nvmlDeviceArchitecture_t *arch);

nvmlReturn_t call_nvmlDeviceGetArchitecture(void *func, nvmlDevice_t device, nvmlDeviceArchitecture_t *arch) {
	return ((nvmlDeviceGetArchitecture_type) func)(device, arch);
}
*/
import "C"
import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

func platformInit() error {
	nvmlLib := C.dlopen(C.CString("libnvidia-ml.so"), C.RTLD_LAZY)
	if nvmlLib == nil {
		return fmt.Errorf("failed opening nvml library")
	}

	resolved := map[string]unsafe.Pointer{}
	for _, sym := range requiredSymbols {
		symbol := C.dlsym(nvmlLib, C.CString(sym))
		if symbol == nil {
			return fmt.Errorf("failed resolving required symbol %q", sym)
		}
		resolved[sym] = symbol
	}
	for _, sym := range optionalSymbols {
		symbol := C.dlsym(nvmlLib, C.CString(sym))
		if symbol == nil {
			continue
		}
		resolved[sym] = symbol
	}
	initV2 := resolved[symbolNvmlInit_v2]
	nvmlInit = func() error {
		rc := nvmlError(C.call_nvmlInit_v2(initV2))
		if errors.Is(rc, NVML_SUCCESS) {
			return nil
		}
		return rc
	}
	getVersion := resolved[symbolNvmlSystemGetNVMLVersion]
	nvmlSystemGetNVMLVersion = func() (string, error) {
		var buf [16]byte
		rc := nvmlError(C.call_nvmlSystemGetNVMLVersion(
			getVersion,
			(*C.char)(unsafe.Pointer(&buf[0])),
			C.uint(len(buf))),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return strings.ReplaceAll(string(buf[:]), "\000", ""), nil
		}
		return "", rc
	}
	countV2 := resolved[symbolNvmlDeviceGetCount_v2]
	nvmlDeviceGetCount = func() (uint64, error) {
		var count C.uint
		rc := nvmlError(C.call_nvmlDeviceGetCount_v2(countV2, &count))
		if errors.Is(rc, NVML_SUCCESS) {
			return uint64(count), nil
		}
		return 0, rc
	}
	getHandleV2 := resolved[symbolNvmlDeviceGetHandleByIndex_v2]
	nvmlDeviceGetHandleByIndex = func(i uint64) (uintptr, error) {
		var handle uintptr
		rc := nvmlError(C.call_nvmlDeviceGetHandleByIndex_v2(
			getHandleV2,
			C.uint(i),
			(*C.nvmlDevice_t)(unsafe.Pointer(&handle))),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return handle, nil
		}
		return 0, rc
	}
	getName := resolved[symbolNvmlDeviceGetName]
	nvmlDeviceGetName = func(device uintptr) (string, error) {
		var buf [96]byte
		bufLen := C.uint(len(buf))
		rc := nvmlError(C.call_nvmlDeviceGetName(
			getName,
			*(*C.nvmlDevice_t)(unsafe.Pointer(&device)),
			(*C.char)(unsafe.Pointer(&buf[0])),
			&bufLen),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return strings.ReplaceAll(string(buf[:]), "\000", ""), nil
		}
		return "", rc
	}
	getPower := resolved[symbolNvmlDeviceGetPowerUsage]
	nvmlDeviceGetPowerUsage = func(device uintptr) (uint32, error) {
		var power uint32
		rc := nvmlError(C.call_nvmlDeviceGetPowerUsage(
			getPower,
			*(*C.nvmlDevice_t)(unsafe.Pointer(&device)),
			(*C.uint)(unsafe.Pointer(&power))),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return power, nil
		}
		return 0, rc
	}
	// Optional symbols
	if getEnergy, ok := resolved[symbolNvmlDeviceGetTotalEnergyConsumption]; ok {
		nvmlDeviceGetTotalEnergyConsumption = func(device uintptr) (uint64, error) {
			var energy uint64
			rc := nvmlError(C.call_nvmlDeviceGetTotalEnergyConsumption(
				getEnergy,
				*(*C.nvmlDevice_t)(unsafe.Pointer(&device)),
				(*C.ulonglong)(unsafe.Pointer(&energy))),
			)
			if errors.Is(rc, NVML_SUCCESS) {
				return energy, nil
			}
			return 0, rc
		}
	}
	if getArch, ok := resolved[symbolNvmlDeviceGetArchitecture]; ok {
		nvmlDeviceGetArchitecture = func(device uintptr) (nvmlDeviceArchitecture, error) {
			var arch nvmlDeviceArchitecture
			rc := nvmlError(C.call_nvmlDeviceGetArchitecture(
				getArch,
				*(*C.nvmlDevice_t)(unsafe.Pointer(&device)),
				(*C.nvmlDeviceArchitecture_t)(unsafe.Pointer(&arch))),
			)
			if errors.Is(rc, NVML_SUCCESS) {
				return arch, nil
			}
			return 0, rc
		}
	}
	return nil
}
