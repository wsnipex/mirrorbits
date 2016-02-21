// Copyright (c) 2014-2015 Ludovic Fauvet
// Licensed under the MIT license

package utils

import (
	"fmt"
	"github.com/etix/mirrorbits/core"
	"github.com/etix/mirrorbits/network"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	DegToRad = 0.017453292519943295769236907684886127134428718885417 // N[Pi/180, 50]
	RadToDeg = 57.295779513082320876798154814105170332405472466564   // N[180/Pi, 50]
)

// Add a trailing slash to the URL
func NormalizeURL(url string) string {
	if url != "" && !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return url
}

// Return the distance in km between two coordinates
func GetDistanceKm(lat1, lon1, lat2, lon2 float32) float32 {
	var R float32 = 6371 // radius of the earth in Km
	dLat := (lat2 - lat1) * float32(DegToRad)
	dLon := (lon2 - lon1) * float32(DegToRad)
	a := math.Sin(float64(dLat/2))*math.Sin(float64(dLat/2)) + math.Cos(float64(lat1*DegToRad))*math.Cos(float64(lat2*DegToRad))*math.Sin(float64(dLon/2))*math.Sin(float64(dLon/2))

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * float32(c)
}

func Min(v1, v2 int) int {
	if v1 < v2 {
		return v1
	}
	return v2
}

func Max(v1, v2 int) int {
	if v1 > v2 {
		return v1
	}
	return v2
}

func Add(x, y int) int {
	return x + y
}

func Version() string {
	return core.VERSION
}

// Return true is `a` is contained in `list`
// Warning: this is slow, don't use it for long datasets
func IsInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func IsAdditionalCountry(clientInfo network.GeoIPRecord, list []string) bool {
	if clientInfo.GeoIPRecord == nil {
		return false
	}
	for i, b := range list {
		if i > 0 && b == clientInfo.CountryCode {
			return true
		}
	}
	return false
}

func IsPrimaryCountry(clientInfo network.GeoIPRecord, list []string) bool {
	if clientInfo.GeoIPRecord == nil {
		return false
	}
	if len(list) > 0 && list[0] == clientInfo.CountryCode {
		return true
	}
	return false
}

// Return true if a stop has been requested
func IsStopped(stop chan bool) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// Return a file size in a human readable form
func ReadableSize(value int64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB"}

	v := float64(value)

	for _, u := range units {
		if v < 1024 || u == "TB" {
			return fmt.Sprintf("%3.1f %s", v, u)
		}
		v /= 1024
	}
	return ""
}

func ElapsedSec(lastTimestamp int64, elapsedTime int64) bool {
	if lastTimestamp+elapsedTime < time.Now().UTC().Unix() {
		return true
	}
	return false
}

func Plural(value interface{}) string {
	n, ok := value.(int)
	if ok && n > 1 || n < -1 {
		return "s"
	}
	return ""
}

func GetHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "hostname"
	}
	return hostname
}

// TimeKeyCoverage returns a slice of strings covering the date range
// used in the redis backend.
func TimeKeyCoverage(start, end time.Time) (dates []string) {
	if start.Day() == end.Day() && start.Month() == end.Month() && start.Year() == end.Year() {
		dates = append(dates, start.Format("2006_01_02"))
		return
	}

	if start.Day() != 1 {
		month := start.Month()
		for {
			if start.Month() != month || start.Equal(end) {
				break
			}
			dates = append(dates, start.Format("2006_01_02"))
			start = start.AddDate(0, 0, 1)
		}
	}

	for {
		tmpyear := time.Date(start.Year()+1, 1, 1, 0, 0, 0, 0, start.Location())
		tmpmonth := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, start.Location())
		if start.Day() == 1 && start.Month() == 1 && (tmpyear.Before(end) || tmpyear.Equal(end)) {
			dates = append(dates, start.Format("2006"))
			start = tmpyear
		} else if tmpmonth.Before(end) || tmpmonth.Equal(end) {
			dates = append(dates, start.Format("2006_01"))
			start = tmpmonth
		} else {
			break
		}
	}

	for {
		if start.AddDate(0, 0, 1).After(end) {
			break
		}
		dates = append(dates, start.Format("2006_01_02"))
		start = start.AddDate(0, 0, 1)
	}

	return
}

// Int64 Sort utility functions
// Copyright (c) 2014 The sortutil Authors, https://github.com/cznic/sortutil/blob/master/sortutil.go#L144

// Int64Slice attaches the methods of sort.Interface to []int64
type Int64Slice []int64

func (s Int64Slice) Len() int {
	return len(s)
}

func (s Int64Slice) Less(i, j int) bool {
	return s[i] < s[j]
}

func (s Int64Slice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s Int64Slice) Sort() {
	sort.Sort(s)
}

func (s Int64Slice) Reverse() {
	sort.Sort(sort.Reverse(s))
}

