package backend

type DataSeries interface {
	Name() string
	Initialized() bool
	Domain() (min int64, max int64)
	RatesBetween(timestampA, timestampB int64) (maximum, mean, minimum, sum float64, ok bool)
	Sum() float64
	RateRange() (min float64, max float64)
}

type WritableDataSeries interface {
	DataSeries
	Insert(Sample) (inserted bool)
}

type Dataset []DataSeries

func (d Dataset) Initialized() bool {
	init := true
	for _, s := range d {
		init = init && s.Initialized()
	}
	return init
}

func (d Dataset) Domain() (dMin int64, dMax int64) {
	for _, s := range d {
		sMin, sMax := s.Domain()
		dMin = min(sMin, dMin)
		dMax = max(sMax, dMax)
	}
	return dMin, dMax
}
