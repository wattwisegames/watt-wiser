package backend

type DataSeries interface {
	Name() string
	Initialized() bool
	Domain() (min int64, max int64)
	Insert(sample Sample) (inserted bool)
	RatesBetween(timestampA, timestampB int64) (maximum, mean, minimum, sum float64, ok bool)
	Sum() float64
	RateRange() (min float64, max float64)
}

type Dataset struct {
	Series []DataSeries
	// seriesMapping maps from series identifiers used by the backend to
	// the index of a series in this structure.
	seriesMapping map[int]int
}

func (d *Dataset) Initialized() bool {
	init := true
	for _, s := range d.Series {
		init = init && s.Initialized()
	}
	return init
}

func (d *Dataset) Domain() (dMin int64, dMax int64) {
	for _, s := range d.Series {
		sMin, sMax := s.Domain()
		dMin = min(sMin, dMin)
		dMax = max(sMax, dMax)
	}
	return dMin, dMax
}

// SetHeadings populates the headings for a dataset. It must be invoked at least once
// prior to the first call to [Insert]. It may be invoked additional times to register
// new data series with their headings.
//
// The series slice provides the backend's ID for each dataset, which is likely to differ
// from the index used to store the data in this type.
func (d *Dataset) SetHeadings(headings []string, series []int) {
	if d.seriesMapping == nil {
		d.seriesMapping = make(map[int]int)
	}
	for i, identifier := range series {
		d.seriesMapping[identifier] = len(d.Series)
		d.Series = append(d.Series, NewSeries(headings[i]))
	}
}

// Insert the sample. Will panic if the sample's Series does not have a heading previously
// registered via [SetHeadings].
func (c *Dataset) Insert(sample Sample) {
	localIdx := c.seriesMapping[sample.Series]
	c.Series[localIdx].Insert(sample)
}
