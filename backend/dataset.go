package backend

type Dataset struct {
	DomainMin int64
	DomainMax int64
	Series    []Series
	Headings  []string
	// seriesMapping maps from series identifiers used by the backend to
	// the index of a series in this structure.
	seriesMapping map[int]int
	initialized   bool
}

func (d *Dataset) Initialized() bool {
	return d.initialized
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
	for _, identifier := range series {
		d.seriesMapping[identifier] = len(d.Series)
		d.Series = append(d.Series, Series{})
	}
	d.Headings = append(d.Headings, headings...)
}

// Insert the sample. Will panic if the sample's Series does not have a heading previously
// registered via [SetHeadings].
func (c *Dataset) Insert(sample Sample) {
	if !c.initialized {
		c.DomainMin = sample.StartTimestampNS
		c.DomainMax = sample.StartTimestampNS
		c.initialized = true
	}
	localIdx := c.seriesMapping[sample.Series]
	c.Series[localIdx].Insert(sample)
	c.DomainMin = min(sample.StartTimestampNS, c.DomainMin)
	c.DomainMax = max(sample.EndTimestampNS, c.DomainMax)
}
