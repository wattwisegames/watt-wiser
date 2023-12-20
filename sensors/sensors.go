package sensors

type Unit uint8

func (u Unit) String() string {
	switch u {
	case Joules:
		return "J"
	case Watts:
		return "W"
	case Amps:
		return "A"
	case Volts:
		return "V"
	default:
		return "?"
	}
}

const (
	Joules Unit = iota
	Watts
	Amps
	Volts
	Unknown
)

const (
	// MicroToUnprefixed is the conversion factor from a micro SI unit to an unprefixed
	// one.
	MicroToUnprefixed = 1.0 / 1_000_000
)

type Sensor interface {
	Name() string
	Unit() Unit
	Read() (float64, error)
}
