// Copyright (c) 2014-2015 Ludovic Fauvet
// Licensed under the MIT license

package utils

import (
	"github.com/etix/geoip"
	"github.com/wsnipex/mirrorbits/network"
	"strings"
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	s := []string{
		"", "",
		"rsync://test.com", "rsync://test.com/",
		"rsync://test.com/", "rsync://test.com/",
	}

	if len(s)%2 != 0 {
		t.Fatal("not multiple of 2")
	}

	for i := 0; i < len(s); i += 2 {
		if r := NormalizeURL(s[i]); r != s[i+1] {
			t.Fatalf("%q: expected %q, got %q", s[i], s[i+1], r)
		}
	}
}

func TestGetDistanceKm(t *testing.T) {
	if r := GetDistanceKm(48.8567, 2.3508, 40.7127, 74.0059); int(r) != 5514 {
		t.Fatalf("Expected 5514, got %f", r)
	}
	if r := GetDistanceKm(48.8567, 2.3508, 48.8567, 2.3508); int(r) != 0 {
		t.Fatalf("Expected 0, got %f", r)
	}
}

func TestMin(t *testing.T) {
	if r := Min(-10, 5); r != -10 {
		t.Fatalf("Expected -10, got %d", r)
	}
}

func TestMax(t *testing.T) {
	if r := Max(-10, 5); r != 5 {
		t.Fatalf("Expected 5, got %d", r)
	}
}

func TestAdd(t *testing.T) {
	if r := Add(2, 40); r != 42 {
		t.Fatalf("Expected 42, got %d", r)
	}
}

func TestIsInSlice(t *testing.T) {
	var b bool
	list := []string{"aaa", "bbb", "ccc"}

	b = IsInSlice("ccc", list)
	if !b {
		t.Fatal("Expected true, got false")
	}
	b = IsInSlice("b", list)
	if b {
		t.Fatal("Expected false, got true")
	}
	b = IsInSlice("", list)
	if b {
		t.Fatal("Expected false, got true")
	}
}

func TestIsAdditionalCountry(t *testing.T) {
	var b bool
	list := []string{"FR", "DE", "GR"}

	clientInfo := network.GeoIPRecord{
		GeoIPRecord: &geoip.GeoIPRecord{
			CountryCode: "FR",
		},
	}

	b = IsAdditionalCountry(clientInfo, list)
	if b {
		t.Fatal("Expected false, got true")
	}

	clientInfo = network.GeoIPRecord{
		GeoIPRecord: &geoip.GeoIPRecord{
			CountryCode: "GR",
		},
	}

	b = IsAdditionalCountry(clientInfo, list)
	if !b {
		t.Fatal("Expected true, got false")
	}
}

func TestIsPrimaryCountry(t *testing.T) {
	var b bool
	list := []string{"FR", "DE", "GR"}

	clientInfo := network.GeoIPRecord{
		GeoIPRecord: &geoip.GeoIPRecord{
			CountryCode: "FR",
		},
	}

	b = IsPrimaryCountry(clientInfo, list)
	if !b {
		t.Fatal("Expected true, got false")
	}

	clientInfo = network.GeoIPRecord{
		GeoIPRecord: &geoip.GeoIPRecord{
			CountryCode: "GR",
		},
	}

	b = IsPrimaryCountry(clientInfo, list)
	if b {
		t.Fatal("Expected false, got true")
	}
}

func TestIsStopped(t *testing.T) {
	stop := make(chan bool, 1)

	if IsStopped(stop) {
		t.Fatal("Expected false, got true")
	}

	stop <- true

	if !IsStopped(stop) {
		t.Fatal("Expected true, got false")
	}
}

func TestReadableSize(t *testing.T) {
	ivalues := []int64{0, 1, 1024, 1000000}
	svalues := []string{"0.0 bytes", "1.0 bytes", "1.0 KB", "976.6 KB"}

	for i, _ := range ivalues {
		if r := ReadableSize(ivalues[i]); r != svalues[i] {
			t.Fatalf("Expected %q, got %q", svalues[i], r)
		}
	}
}

func TestElapsedSec(t *testing.T) {
	now := time.Now().UTC().Unix()

	lastTimestamp := now - 1000

	if ElapsedSec(lastTimestamp, 500) == false {
		t.Fatalf("Expected true, got false")
	}
	if ElapsedSec(lastTimestamp, 5000) == true {
		t.Fatalf("Expected false, got true")
	}
}

func TestPlural(t *testing.T) {
	if Plural(2) != "s" {
		t.Fatalf("Expected 's', got ''")
	}
	if Plural(10000000) != "s" {
		t.Fatalf("Expected 's', got ''")
	}
	if Plural(-2) != "s" {
		t.Fatalf("Expected 's', got ''")
	}
	if Plural(1) != "" {
		t.Fatalf("Expected '', got 's'")
	}
	if Plural(-1) != "" {
		t.Fatalf("Expected '', got 's'")
	}
	if Plural(0) != "" {
		t.Fatalf("Expected '', got 's'")
	}
}

func TestGetHostname(t *testing.T) {
	if len(GetHostname()) == 0 {
		t.Fatalf("Expected something, got nothing")
	}
	if strings.Contains(GetHostname(), " ") {
		t.Fatalf("Expected no space, got a least one space")
	}
}

func TestTimeKeyCoverage(t *testing.T) {
	date_1_start := time.Date(2015, 10, 30, 12, 42, 11, 0, time.UTC)
	date_1_end := time.Date(2015, 12, 2, 13, 42, 11, 0, time.UTC)
	result_1 := []string{"2015_10_30", "2015_10_31", "2015_11", "2015_12_01"}

	result := TimeKeyCoverage(date_1_start, date_1_end)

	if len(result) != len(result_1) {
		t.Fatalf("Expect %d elements, got %d", len(result_1), len(result))
	}

	for i, r := range result {
		if r != result_1[i] {
			t.Fatalf("Expect %#v, got %#v", result_1, result)
		}
	}

	/* */

	date_2_start := time.Date(2015, 12, 2, 12, 42, 11, 0, time.UTC)
	date_2_end := time.Date(2015, 12, 2, 13, 42, 11, 0, time.UTC)
	result_2 := []string{"2015_12_02"}

	result = TimeKeyCoverage(date_2_start, date_2_end)

	if len(result) != len(result_2) {
		t.Fatalf("Expect %d elements, got %d", len(result_2), len(result))
	}

	for i, r := range result {
		if r != result_2[i] {
			t.Fatalf("Expect %#v, got %#v", result_2, result)
		}
	}

	/* */

	date_3_start := time.Date(2015, 1, 1, 12, 42, 11, 0, time.UTC)
	date_3_end := time.Date(2017, 1, 1, 13, 42, 11, 0, time.UTC)
	result_3 := []string{"2015", "2016"}

	result = TimeKeyCoverage(date_3_start, date_3_end)

	if len(result) != len(result_3) {
		t.Fatalf("Expect %d elements, got %d", len(result_3), len(result))
	}

	for i, r := range result {
		if r != result_3[i] {
			t.Fatalf("Expect %#v, got %#v", result_3, result)
		}
	}

	/* */

	date_4_start := time.Date(2015, 12, 31, 12, 42, 11, 0, time.UTC)
	date_4_end := time.Date(2016, 1, 2, 13, 42, 11, 0, time.UTC)
	result_4 := []string{"2015_12_31", "2016_01_01"}

	result = TimeKeyCoverage(date_4_start, date_4_end)

	if len(result) != len(result_4) {
		t.Fatalf("Expect %d elements, got %d", len(result_4), len(result))
	}

	for i, r := range result {
		if r != result_4[i] {
			t.Fatalf("Expect %#v, got %#v", result_4, result)
		}
	}
}
