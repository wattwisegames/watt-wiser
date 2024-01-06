# Watt Wiser

A software energy consumption estimation tool built as part of the [Watt-Wise Game Jam](https://wattwise.games/).

![demo gif](./img/watt-wiser-demo.gif)

## Status

Unstable and experimental, but usable for simple visualization. The GUI is functional on Linux, macOS, and Windows, but the sensors executable that actually collects data currently only supports Linux. Interested users of other OSes can still try the GUI out on the included example trace file.

# GUI

To build the GUI, you'll need the latest version of [Go](https://golang.org/dl) and the [dependencies for Gio](https://gioui.org/doc/install), the GUI toolkit in use.

You can then build the gui with:

```
go build -o watt-wiser
```

To run the GUI against the provided example trace, you can run:

```
./watt-wiser ./example-trace.csv
```

## Controls

You can toggle between a line plot and a stacked area plot by clicking in the chart area. The line plot is useful for comparing the absolute values of different data sources, while the stacked area graph helps estimate total consumption.

You can also toggle on/off individual data sets by clicking on the colored square to the left of that data in the legend. When using the stacked area graph, it's important to toggle off data sets that overlap. See the next section for an example.

Scroll vertically to zoom on the time axis and horizontally to pan.

## Included Example Trace

This repo includes `./example-trace.csv`, a sensor recording from Chris Waldon's desktop. It has an Intel CPU and an AMD GPU, and (at the time of the recording) there were four relevant sensors supported:

- `package-0`: this data is the *sum* of all energy use reported for the CPU, DRAM, and other silicon instrumented by Intel. This data comes from Intel's RAPL technology.
- `core`: this data represents *just* the CPU, but is included within `package-0`'s total.
- `dram`: this data represents *just* the DRAM, but is included within `package-0`'s total.
- `amdgpu`: this data represents the average power draw reported by the AMD GPU via HWMON.

When viewing the data in stacked area mode, `core` and `dram` should be toggled off to get an accurate picture of the measured energy use, otherwise their usage will be counted multiple times in the graph (since they are part of `package-0`).

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
