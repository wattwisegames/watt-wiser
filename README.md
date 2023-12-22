# energy

A software energy consumption estimation tool.

## Status

Unstable and experimental.

## Dependencies

We rely upon `libsensors` to read data from the kernel HWMON subsystem. Install it (and its header files) with your system package manager.

## Usage

Sadly, reading energy info requires elevated permissions. To get full data:

```
go build && sudo ./energy -sensors | ./energy
```

## License

UNLICENSE
