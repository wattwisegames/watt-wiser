package backend

import (
	"sort"

	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
)

// Series represents one data set in a visualization.
type Series struct {
	startTimestamps            []int64
	endTimestamps              []int64
	values                     []float64
	RangeRateMax, RangeRateMin float64
	domainMin, domainMax       int64
	Sum                        float64
	name                       string
	initialized                bool
}

func NewSeries(name string) *Series {
	return &Series{name: name}
}

func (s *Series) Name() string {
	return s.name
}

func (s *Series) Initialized() bool {
	return s.initialized
}

func (s *Series) Domain() (min int64, max int64) {
	return s.domainMin, s.domainMax
}

// Insert adds a value at a given timestamp to the series. In the event
// that the series already contains a value at that time, nothing is added
// and the method returns false. Otherwise, the method returns true.
func (s *Series) Insert(sample Sample) (inserted bool) {
	if !s.initialized {
		s.domainMin = sample.StartTimestampNS
		s.domainMax = sample.StartTimestampNS
		s.initialized = true
	}
	if len(s.endTimestamps) > 0 && s.endTimestamps[len(s.endTimestamps)-1] > sample.StartTimestampNS {
		// Reject samples with times overlapping the existing data in the series.
		return false
	}
	s.domainMin = min(sample.StartTimestampNS, s.domainMin)
	s.domainMax = max(sample.EndTimestampNS, s.domainMax)
	var rate float64
	var quantity float64
	duration := float64(sample.EndTimestampNS - sample.StartTimestampNS)
	durationSeconds := duration / 1_000_000_000
	if sample.Unit == sensors.Joules {
		rate = (sample.Value / durationSeconds)
		quantity = sample.Value
	} else if sample.Unit == sensors.Watts {
		rate = sample.Value
		quantity = (sample.Value * duration) / 1_000_000_000
	}

	if len(s.startTimestamps) < 1 {
		s.RangeRateMax = rate
		s.RangeRateMin = rate
	} else {
		s.RangeRateMax = max(s.RangeRateMax, rate)
		s.RangeRateMin = min(s.RangeRateMin, rate)
	}
	s.startTimestamps = append(s.startTimestamps, sample.StartTimestampNS)
	s.endTimestamps = append(s.endTimestamps, sample.EndTimestampNS)
	s.values = append(s.values, quantity)

	s.Sum += quantity
	return true
}

// RatesBetween returns statistics about the rate of consumption in the half-open time interval
// [timestampA,timestampB). If is no data in the series, this method will
// always return zero. If timestampA is less than timestampB, the half open
// interval [timestampB,timestampA) will be returned. If the interval extends
// beyond the domain of the data, all data return values will be zero and the
// ok return value will be false.
func (s *Series) RatesBetween(timestampA, timestampB int64) (maximum, mean, minimum, sum float64, ok bool) {
	if len(s.startTimestamps) < 1 {
		return 0, 0, 0, 0, false
	}
	if timestampB < timestampA {
		timestampA, timestampB = timestampB, timestampA
	}
	indexA := sort.Search(len(s.startTimestamps), func(i int) bool {
		return timestampA < s.endTimestamps[i]
	})
	if indexA == len(s.startTimestamps) {
		return 0, 0, 0, 0, false
	}
	indexB := sort.Search(len(s.startTimestamps), func(i int) bool {
		return timestampB < s.endTimestamps[i]
	})
	if indexB == len(s.startTimestamps) {
		lastEnd := s.endTimestamps[len(s.endTimestamps)-1]
		if timestampB > lastEnd {
			return 0, 0, 0, 0, false
		}
		// If the last timestamp is exactly equal to the end of the final time, then we can proceed.
		indexB--
	}
	if indexA == indexB {
		v := s.values[indexA]
		interval := float64(s.endTimestamps[indexA] - s.startTimestamps[indexA])
		queryInterval := timestampB - timestampA
		mean := v / (interval / 1_000_000_000)
		ok = true
		return mean, mean, mean, (v * (float64(queryInterval) / interval)), ok
	}
	values := s.values[indexA : indexB+1]
	hasExtrema := false
	for i, v := range values {
		interval := float64(s.endTimestamps[indexA+i] - s.startTimestamps[indexA+i])
		if i == 0 || i == len(values)-1 {
			var querySampleInterval int64
			if i == 0 {
				querySampleInterval = s.endTimestamps[indexA] - timestampA
			} else if i == len(values)-1 {
				querySampleInterval = timestampB - s.startTimestamps[indexB]
			}
			if querySampleInterval == 0 {
				continue
			}
			// Scale the value by the proportion of the sample that is within
			// the queried period.
			ratio := float64(querySampleInterval) / interval
			v = v * ratio
			interval = float64(querySampleInterval)
		}
		mean += v
		rate := v / (interval / 1_000_000_000)
		if hasExtrema {
			maximum = max(maximum, rate)
			minimum = min(minimum, rate)
		} else {
			maximum = rate
			minimum = rate
			hasExtrema = true
		}
	}
	sum = mean
	mean /= (float64(timestampB-timestampA) / 1_000_000_000)
	return maximum, mean, minimum, sum, true
}
