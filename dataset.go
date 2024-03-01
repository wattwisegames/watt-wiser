package main

import "git.sr.ht/~whereswaldon/watt-wiser/backend"

type Dataset struct {
	DomainMin int64
	DomainMax int64
	Series    []Series
	Headings  []string
	// seriesMapping maps from series identifiers used by the backend to
	// the index of a series in this structure.
	seriesMapping map[int]int
}

func (d *Dataset) Initialized() bool {
	return len(d.Headings) != 0 && len(d.Series) != 0
}

func (d *Dataset) SetHeadings(headings []string, series []int) {
	if d.seriesMapping == nil {
		d.seriesMapping = make(map[int]int)
	}
	for idx, identifier := range series {
		d.seriesMapping[identifier] = len(d.Headings) + idx
	}
	d.Headings = headings
}

func (c *Dataset) Insert(sample backend.Sample) {
	if len(c.Series) == 0 {
		c.DomainMin = sample.StartTimestampNS
		c.DomainMax = sample.StartTimestampNS
	}
	for sample.Series >= len(c.Series) {
		c.Series = append(c.Series, Series{})
	}
	c.Series[c.seriesMapping[sample.Series]].Insert(sample)
	c.DomainMin = min(sample.StartTimestampNS, c.DomainMin)
	c.DomainMax = max(sample.EndTimestampNS, c.DomainMax)
}
