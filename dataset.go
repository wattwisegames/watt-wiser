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
	}
	for sample.Series >= len(c.Series) {
		c.Series = append(c.Series, Series{})
	}
	c.Series[sample.Series].Insert(sample)
	c.DomainMin = min(sample.StartTimestampNS, c.DomainMin)
	c.DomainMax = max(sample.EndTimestampNS, c.DomainMax)
}
