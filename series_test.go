package main

import "testing"

func TestSeries(t *testing.T) {
	s := Series{}
	interval := int64(1000)
	sampleCount := int64(10)
	expectedSum := float64(0)
	for i := int64(0); i < sampleCount; i++ {
		ok := s.Insert(i*interval, (i+1)*interval, float64(i))
		if !ok {
			t.Errorf("inserting non-overlapping samples should always be okay, but sample %d failed", i)
		}
		expectedSum += float64(i)
	}
	halfSample := interval / 2
	sum := float64(0)
	for i := int64(0); i < sampleCount*2; i++ {
		max, mean, min, ok := s.RatesBetween(i*halfSample, (i+1)*halfSample)
		if !ok {
			t.Errorf("querying values in range should always be okay, value %d was not", i)
		}
		if min != mean || mean != max {
			t.Errorf("min, mean, and max should all be equal within one sample, value %d has %f %f %f", i, min, mean, max)
		}
		sum += mean * (float64(halfSample) / 1_000_000_000)
	}
	if sum != expectedSum {
		t.Errorf("expected %f total consumed, got %f", expectedSum, sum)
	}
}

func TestSeriesGap(t *testing.T) {
	type datapoint struct {
		start, end int64
		value      float64
	}
	type expectation struct {
		start, end     int64
		max, mean, min float64
		ok             bool
	}
	type testcase struct {
		name   string
		data   []datapoint
		expect []expectation
	}
	for _, tc := range []testcase{
		{
			name: "holes in data",
			data: []datapoint{
				{
					start: 0,
					end:   1000,
					value: 1,
				},
				{
					start: 2000,
					end:   3000,
					value: 3,
				},
				{
					start: 4000,
					end:   5000,
					value: 5,
				},
			},
			expect: []expectation{
				{
					ok:    true,
					start: 500,
					end:   1500,
					max:   1e6,
					min:   1e6,
					mean:  1e6,
				},
				{
					ok:    false,
					start: 1000,
					end:   2000,
					max:   0,
					min:   0,
					mean:  0,
				},
				{
					ok:    true,
					start: 1500,
					end:   2500,
					max:   3e6,
					min:   3e6,
					mean:  3e6,
				},
				{
					ok:    true,
					start: 500,
					end:   4500,
					max:   5e6,
					min:   1e6,
					mean:  3e6,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Series{}
			for _, p := range tc.data {
				s.Insert(p.start, p.end, p.value)
			}
			for i, e := range tc.expect {
				max, mean, min, ok := s.RatesBetween(e.start, e.end)
				if e.ok != ok {
					t.Errorf("[%d] expected ok %v, got %v", i, e.ok, ok)
				}
				if e.max != max {
					t.Errorf("[%d] expected max %v, got %v", i, e.max, max)
				}
				if e.mean != mean {
					t.Errorf("[%d] expected mean %v, got %v", i, e.mean, mean)
				}
				if e.min != min {
					t.Errorf("[%d] expected min %v, got %v", i, e.min, min)
				}
			}
		})
	}
}
