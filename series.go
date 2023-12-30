package main

import (
	"slices"
)

// Series represents one data set in a visualization.
type Series struct {
	startTimestamps    []int64
	endTimestamps      []int64
	values             []float64
	RangeMax, RangeMin float64
	Sum                float64
}

// Insert adds a value at a given timestamp to the series. In the event
// that the series already contains a value at that time, nothing is added
// and the method returns false. Otherwise, the method returns true.
func (s *Series) Insert(startTimestamp, endTimestamp int64, value float64) (inserted bool) {
	if len(s.startTimestamps) < 1 {
		s.RangeMax = value
		s.RangeMin = value
	}
	index, found := slices.BinarySearch(s.startTimestamps, startTimestamp)
	if found {
		return false
	}
	s.startTimestamps = slices.Insert(s.startTimestamps, index, startTimestamp)
	s.endTimestamps = slices.Insert(s.endTimestamps, index, endTimestamp)
	s.values = slices.Insert(s.values, index, value)
	s.RangeMax = max(s.RangeMax, value)
	s.RangeMin = min(s.RangeMin, value)
	s.Sum += value
	return true
}

// Values returns statistics about the data in the half-open time interval
// [timestampA,timestampB). If is no data in the series, this method will
// always return zero. If timestampA is less than timestampB, the half open
// interval [timestampB,timestampA) will be returned. If the interval extends
// beyond the domain of the data, all data return values will be zero and the
// ok return value will be false.
func (s *Series) Values(timestampA, timestampB int64) (maximum, mean, minimum float64, ok bool) {
	if len(s.startTimestamps) < 1 {
		return 0, 0, 0, false
	}
	if timestampB < timestampA {
		timestampA, timestampB = timestampB, timestampA
	}
	queryInterval := timestampB - timestampA
	indexA, _ := slices.BinarySearch(s.startTimestamps, timestampA)
	indexB, _ := slices.BinarySearch(s.startTimestamps, timestampB)
	if indexA > 0 && s.startTimestamps[indexA] > timestampA {
		// The sample prior to the search result
		indexA--
	}
	if indexB > 0 && s.startTimestamps[indexB] > timestampB {
		// The sample prior to the search result
		indexB--
	}
	if indexA == indexB {
		var v float64
		if indexA == len(s.startTimestamps) {
			return v, v, v, false
		} else if indexA < 0 {
			return v, v, v, false
		}
		v = s.values[indexA]
		interval := s.endTimestamps[indexA] - s.startTimestamps[indexA]
		ratio := float64(queryInterval) / float64(interval)
		mean := (v * ratio) / (float64(queryInterval) / 1_000_000_000)
		ok = true
		return v, mean, v, ok
	}
	values := s.values[indexA : indexB+1]
	maximum = values[0]
	minimum = values[0]
	for i, v := range values {
		if i == 0 {
			interval := s.endTimestamps[indexA] - s.startTimestamps[indexA]
			querySampleInterval := s.endTimestamps[indexA] - timestampA
			ratio := float64(querySampleInterval) / float64(interval)
			mean += v * ratio
		} else if i == len(values)-1 {
			interval := s.endTimestamps[indexB] - s.startTimestamps[indexB]
			querySampleInterval := timestampB - s.startTimestamps[indexB]
			ratio := float64(querySampleInterval) / float64(interval)
			mean += v * ratio
		} else {
			mean += v
		}
		maximum = max(maximum, v)
		minimum = min(minimum, v)
	}
	mean /= (float64(queryInterval) / 1_000_000_000)
	return maximum, mean, minimum, true
}
