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

typedef nvmlReturn_t (*nvmlDeviceGetArchitecture_type)(nvmlDevice_t device, nvmlDeviceArchitecture_t *arch);

nvmlReturn_t call_nvmlDeviceGetArchitecture(void *func, nvmlDevice_t device, nvmlDeviceArchitecture_t *arch) {
	return ((nvmlDeviceGetArchitecture_type) func)(device, arch);
}
*/
import "C"
import (
	"errors"
	"fmt"
	"unsafe"
)

func platformInit() error {
	nvmlLib := C.dlopen(C.CString("libnvidia-ml.so"), C.RTLD_LAZY)
	if nvmlLib == nil {
		return fmt.Errorf("failed opening nvml library")
	}
	initV2 := C.dlsym(nvmlLib, C.CString(symbolNvmlInit_v2))
	if initV2 == nil {
		return fmt.Errorf("failed resolving nvmlInit_v2")
	}
	nvmlInit = func() error {
		rc := nvmlError(C.call_nvmlInit_v2(initV2))
		if errors.Is(rc, NVML_SUCCESS) {
			return nil
		}
		return rc
	}
	getVersion := C.dlsym(nvmlLib, C.CString(symbolNvmlSystemGetNVMLVersion))
	if getVersion == nil {
		return fmt.Errorf("failed resolving nvmlSystemGetNVMLVersion")
	}
	nvmlSystemGetNVMLVersion = func() (string, error) {
		var buf [16]byte
		rc := nvmlError(C.call_nvmlSystemGetNVMLVersion(
			getVersion,
			(*C.char)(unsafe.Pointer(&buf[0])),
			C.uint(len(buf))),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return string(buf[:]), nil
		}
		return "", rc
	}
	countV2 := C.dlsym(nvmlLib, C.CString(symbolNvmlDeviceGetCount_v2))
	if countV2 == nil {
		return fmt.Errorf("failed resolving nvml count func")
	}
	nvmlDeviceGetCount = func() (uint64, error) {
		var count C.uint
		rc := nvmlError(C.call_nvmlDeviceGetCount_v2(countV2, &count))
		if errors.Is(rc, NVML_SUCCESS) {
			return uint64(count), nil
		}
		return 0, rc
	}
	getHandleV2 := C.dlsym(nvmlLib, C.CString(symbolNvmlDeviceGetHandleByIndex_v2))
	if countV2 == nil {
		return fmt.Errorf("failed resolving nvml get device handle func")
	}
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
	getEnergy := C.dlsym(nvmlLib, C.CString(symbolNvmlDeviceGetTotalEnergyConsumption))
	if countV2 == nil {
		return fmt.Errorf("failed resolving nvml get energy func")
	}
	nvmlDeviceGetTotalEnergyConsumption = func(device uintptr) (uint64, error) {
		var energy uint64
		rc := nvmlError(C.call_nvmlDeviceGetTotalEnergyConsumption(
			getEnergy,
			*(*C.nvmlDevice_t)(unsafe.Pointer(device)),
			(*C.ulonglong)(unsafe.Pointer(&energy))),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return energy, nil
		}
		return 0, rc
	}
	getPower := C.dlsym(nvmlLib, C.CString(symbolNvmlDeviceGetPowerUsage))
	if getPower == nil {
		return fmt.Errorf("failed resolving nvml get power func")
	}
	nvmlDeviceGetPowerUsage = func(device uintptr) (uint32, error) {
		var power uint32
		rc := nvmlError(C.call_nvmlDeviceGetPowerUsage(
			getPower,
			*(*C.nvmlDevice_t)(unsafe.Pointer(device)),
			(*C.uint)(unsafe.Pointer(&power))),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return power, nil
		}
		return 0, rc
	}
	getArch := C.dlsym(nvmlLib, C.CString(symbolNvmlDeviceGetArchitecture))
	if getArch == nil {
		return fmt.Errorf("failed resolving nvml get architecture func")
	}
	nvmlDeviceGetArchitecture = func(device uintptr) (nvmlDeviceArchitecture, error) {
		var arch nvmlDeviceArchitecture
		rc := nvmlError(C.call_nvmlDeviceGetArchitecture(
			getArch,
			*(*C.nvmlDevice_t)(unsafe.Pointer(device)),
			(*C.nvmlDeviceArchitecture_t)(unsafe.Pointer(&arch))),
		)
		if errors.Is(rc, NVML_SUCCESS) {
			return arch, nil
		}
		return 0, rc
	}
	return nil
}
