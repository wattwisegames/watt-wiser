# Watt Wiser

A software energy consumption estimation tool built as part of the [Watt-Wise Game Jam](https://wattwise.games/).

![demo gif](./img/watt-wiser-demo.gif)

## Status

Unstable and experimental, but usable for simple visualization. The GUI is functional on Linux, macOS, and Windows, but the sensors executable that actually collects data currently only supports Linux. Interested users of other OSes can still try the GUI out on the included example trace file.

# GUI

To build the GUI, you'll need the latest version of [Go](https://golang.org/dl) and the [dependencies for Gio](https://gioui.org/doc/install/), the GUI toolkit in use.

You can then build the gui with:

```
go build -o watt-wiser
```

To run the GUI against the provided example trace, you can run:

```
./watt-wiser ./example-trace.csv
```

# Sensors

The sensors executable is currently only supported on Linux, but we'd like to expand it to support all platforms. If you're interested in helping out, please get in touch!

We rely upon `libsensors` to read data from the kernel HWMON subsystem. Install it (and its header files) with your system package manager. You'll also need a functioning C toolchain. You can then build the sensors with:

```
go build -o watt-wiser-sensors ./cmd/watt-wiser-sensors
```

Sadly, accessing RAPL (a good source of CPU energy data) requires root, so the sensors executable needs to be run as root:

```
sudo ./watt-wiser-sensors | tee energy.csv
```

If you've already built the GUI, you can visualize the data live with:


```
sudo ./watt-wiser-sensors | tee energy.csv | watt-wiser
```

## License

UNLICENSE
