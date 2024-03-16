package backend

import (
	"testing"
)

func TestSeries(t *testing.T) {
	s := Series{}
	interval := int64(1000)
	sampleCount := int64(10)
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
