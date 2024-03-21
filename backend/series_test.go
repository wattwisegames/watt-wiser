package backend

import (
	"testing"
)

func makeTestSeries(t *testing.T, interval, sampleCount int64) (*Series, float64) {
	s := Series{}
	expectedSum := float64(0)
	for i := int64(0); i < sampleCount; i++ {
		sample := Sample{
			StartTimestampNS: i * interval,
			EndTimestampNS:   (i + 1) * interval,
			Value:            float64(i),
		}
		ok := s.Insert(sample)
		if !ok {
			t.Errorf("inserting non-overlapping samples should always be okay, but sample %d failed", i)
		}
		expectedSum += float64(i)
	}
	return &s, expectedSum
}

func TestBenchmarkSeries(t *testing.T) {
	interval := int64(1_000_000_000)
	sampleCount := int64(10)
	s, _ := makeTestSeries(t, interval, sampleCount)

	bs := NewBenchmarkSeriesFrom(s, BenchmarkData{
		PreBaselineStart:  interval * 2,
		PreBaselineEnd:    interval * 4,
		PostBaselineStart: interval * 6,
		PostBaselineEnd:   interval * 8,
	})

	actualSum := bs.Sum()
	expectedSum := 0.0
	if actualSum != expectedSum {
		t.Errorf("expected sum %f, got %f", expectedSum, actualSum)
	}
	dMin, dMax := bs.Domain()
	expectedDMin := 0
	expectedDMax := interval * 6
	if dMin != int64(expectedDMin) {
		t.Errorf("expected domain min %d, got %d", expectedDMin, dMin)
	}
	if dMax != int64(expectedDMax) {
		t.Errorf("expected domain max %d, got %d", expectedDMax, dMax)
	}
	rMin, rMax := bs.RateRange()
	expectedRMin := -4.5
	expectedRMax := 4.5
	if rMin != expectedRMin {
		t.Errorf("expected range min %f, got %f", expectedRMin, rMin)
	}
	if rMax != expectedRMax {
		t.Errorf("expected range max %f, got %f", expectedRMax, rMax)
	}

	type res struct {
		start, end          int64
		max, mean, min, sum float64
		ok                  bool
	}
	for _, r := range []res{
		{
			start: -interval,
			end:   0,
			ok:    false,
		},
		{
			start: 0,
			end:   interval,
			max:   -2.5,
			mean:  -2.5,
			min:   -2.5,
			sum:   -2.5,
			ok:    true,
		},
		{
			start: 0,
			end:   interval * 2,
			max:   -1.5,
			mean:  -2.0,
			min:   -2.5,
			sum:   -4.0,
			ok:    true,
		},
		{
			start: interval * 2,
			end:   interval * 5,
			max:   1.5,
			mean:  0.5,
			min:   -0.5,
			sum:   1.5,
			ok:    true,
		},
	} {
		maximum, mean, minimum, sum, ok := bs.RatesBetween(r.start, r.end)
		if ok != r.ok {
			t.Errorf("expected in-range data to be okay")
		}
		if maximum != r.max {
			t.Errorf("expected range max %f, got %f", r.max, maximum)
		}
		if minimum != r.min {
			t.Errorf("expected range min %f, got %f", r.min, minimum)
		}
		if mean != r.mean {
			t.Errorf("expected range mean %f, got %f", r.mean, mean)
		}
		if sum != r.sum {
			t.Errorf("expected range sum %f, got %f", r.sum, sum)
		}
	}
}

func TestSeries(t *testing.T) {
	interval := int64(1000)
	sampleCount := int64(10)
	s, expectedSum := makeTestSeries(t, interval, sampleCount)
	halfSample := interval / 2
	sum := float64(0)
	for i := int64(0); i < sampleCount*2; i++ {
		max, mean, min, _, ok := s.RatesBetween(i*halfSample, (i+1)*halfSample)
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
