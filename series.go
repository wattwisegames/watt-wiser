package main

import "slices"

// Series represents one data set in a visualization.
type Series struct {
	timestamps         []int64
	values             []float64
	RangeMax, RangeMin float64
	Sum                float64
}

// Insert adds a value at a given timestamp to the series. In the event
// that the series already contains a value at that time, nothing is added
// and the method returns false. Otherwise, the method returns true.
func (s *Series) Insert(timestamp int64, value float64) (inserted bool) {
	if len(s.timestamps) < 1 {
		s.RangeMax = value
		s.RangeMin = value
	}
	index, found := slices.BinarySearch(s.timestamps, timestamp)
	if found {
		return false
	}
	s.timestamps = slices.Insert(s.timestamps, index, timestamp)
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
// beyond the domain of the data, the data of the nearest datapoint will be used,
// but the ok return will be false.
func (s *Series) Values(timestampA, timestampB int64) (maximum, mean, minimum float64, ok bool) {
	if len(s.timestamps) < 1 {
		return 0, 0, 0, false
	}
	if timestampB < timestampA {
		timestampA, timestampB = timestampB, timestampA
	}
	indexA, _ := slices.BinarySearch(s.timestamps, timestampA)
	indexB, _ := slices.BinarySearch(s.timestamps, timestampB)
	if indexA == indexB {
		var v float64
		if indexA == len(s.timestamps) {
			v = s.values[len(s.values)-1]
			ok = false
		} else if indexA < 0 {
			v = s.values[0]
			ok = false
		} else {
			v = s.values[indexA]
			ok = true
		}
		return v, v, v, ok
	}
	values := s.values[indexA:indexB]
	maximum = values[0]
	minimum = values[0]
	for _, v := range values {
		mean += v
		maximum = max(maximum, v)
		minimum = min(minimum, v)
	}
	mean /= float64(len(values))
	return maximum, mean, minimum, true
}
