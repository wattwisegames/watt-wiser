package backend

import (
	"sort"
	"sync"

	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
)

type BenchmarkSeries struct {
	wrapped      DataSeries
	bd           BenchmarkData
	baselineRate float64
	baselineSum  float64
	namePrefix   string
}

var _ DataSeries = (*BenchmarkSeries)(nil)

func NewBenchmarkSeriesFrom(series DataSeries, bd BenchmarkData) *BenchmarkSeries {
	b := &BenchmarkSeries{
		namePrefix: bd.BenchmarkID + " ",
		wrapped:    series,
		bd:         bd,
	}
	_, preMean, _, _, _ := series.RatesBetween(bd.PreBaselineStart, bd.PreBaselineEnd)
	_, postMean, _, _, _ := series.RatesBetween(bd.PostBaselineStart, bd.PostBaselineEnd)
	_, _, _, sum, _ := series.RatesBetween(bd.PreBaselineStart, bd.PostBaselineEnd)
	b.baselineRate = (preMean + postMean) / 2
	b.baselineSum = sum - (float64(b.bd.PostBaselineEnd-b.bd.PreBaselineStart)*b.baselineRate)/1_000_000_000
	return b
}

func (b *BenchmarkSeries) Initialized() bool {
	return b.wrapped.Initialized()
}

func (b *BenchmarkSeries) Domain() (min, max int64) {
	return 0, b.bd.PostBaselineEnd - b.bd.PreBaselineStart
}

func (b *BenchmarkSeries) RateRange() (min, max float64) {
	min, max = b.wrapped.RateRange()
	min -= b.baselineRate
	max -= b.baselineRate
	return min, max
}

func (b *BenchmarkSeries) Name() string {
	return b.namePrefix + b.wrapped.Name()
}

func (b *BenchmarkSeries) Sum() float64 {
	return b.baselineSum
}

func (b *BenchmarkSeries) RatesBetween(timestampA, timestampB int64) (maximum, mean, minimum, sum float64, ok bool) {
	if timestampA <= 0 && timestampB <= 0 {
		return 0, 0, 0, 0, false
	}
	// Normalize the times so that time zero is the baseline start.
	timestampA += b.bd.PreBaselineStart
	timestampB += b.bd.PreBaselineStart
	// Query real values.
	maximum, mean, minimum, sum, ok = b.wrapped.RatesBetween(timestampA, timestampB)
	// Factor out baseline usage.
	maximum -= b.baselineRate
	minimum -= b.baselineRate
	mean -= b.baselineRate
	sum -= (b.baselineRate * float64(timestampB-timestampA)) / 1_000_000_000
	return maximum, mean, minimum, sum, ok
}

// Series represents one data set in a visualization.
type Series struct {
	lock                       sync.RWMutex
	startTimestamps            []int64
	endTimestamps              []int64
	values                     []float64
	rangeRateMax, rangeRateMin float64
	domainMin, domainMax       int64
	sum                        float64
	name                       string
	initialized                bool
}

func NewSeries(name string) *Series {
	return &Series{name: name}
}

func (s *Series) Name() string {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.name
}

func (s *Series) Initialized() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.initialized
}

func (s *Series) Domain() (min int64, max int64) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.domainMin, s.domainMax
}

func (s *Series) RateRange() (min float64, max float64) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.rangeRateMin, s.rangeRateMax
}

func (s *Series) Sum() float64 {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.sum
}

// Insert adds a value at a given timestamp to the series. In the event
// that the series already contains a value at that time, nothing is added
// and the method returns false. Otherwise, the method returns true.
func (s *Series) Insert(sample Sample) (inserted bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
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
		s.rangeRateMax = rate
		s.rangeRateMin = rate
	} else {
		s.rangeRateMax = max(s.rangeRateMax, rate)
		s.rangeRateMin = min(s.rangeRateMin, rate)
	}
	s.startTimestamps = append(s.startTimestamps, sample.StartTimestampNS)
	s.endTimestamps = append(s.endTimestamps, sample.EndTimestampNS)
	s.values = append(s.values, quantity)

	s.sum += quantity
	return true
}

// RatesBetween returns statistics about the rate of consumption in the half-open time interval
// [timestampA,timestampB). If is no data in the series, this method will
// always return zero. If timestampA is less than timestampB, the half open
// interval [timestampB,timestampA) will be returned. If the interval extends
// beyond the domain of the data, all data return values will be zero and the
// ok return value will be false.
func (s *Series) RatesBetween(timestampA, timestampB int64) (maximum, mean, minimum, sum float64, ok bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
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
