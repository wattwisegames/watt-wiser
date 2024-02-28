package main

import "git.sr.ht/~whereswaldon/watt-wiser/backend"

type Dataset struct {
	DomainMin int64
	DomainMax int64
	Series    []Series
	Headings  []string
}

func (d *Dataset) Initialized() bool {
	return len(d.Headings) != 0 && len(d.Series) != 0
}

func (d *Dataset) SetHeadings(headings []string) {
	d.Headings = headings
}

func (c *Dataset) Insert(sample backend.Sample) {
	if len(c.Series) == 0 {
		c.DomainMin = sample.StartTimestampNS
		c.DomainMax = sample.StartTimestampNS
		c.Series = make([]Series, len(sample.Data))
	}
	for i, datum := range sample.Data {
		// RangeMin should probably always be zero, no matter what the sensors say. None of the
		// quantities we're measuring can actually be less than zero.
		//c.RangeMin = min(datum, c.RangeMin)
		if datum < 0 {
			datum = 0
		}
		c.Series[i].Insert(sample.StartTimestampNS, sample.EndTimestampNS, datum)
	}
	c.DomainMin = min(sample.StartTimestampNS, c.DomainMin)
	c.DomainMax = max(sample.StartTimestampNS, c.DomainMax)
}
