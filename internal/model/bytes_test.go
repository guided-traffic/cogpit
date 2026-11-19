package model_test

import (
	"math"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestFormatBytes is D-32's rule: whole bytes up to 1023, from 1024 the next binary unit with one
// decimal, a space before the unit.
func TestFormatBytes(t *testing.T) {
	const (
		kib = int64(1) << 10
		mib = int64(1) << 20
		gib = int64(1) << 30
		tib = int64(1) << 40
		pib = int64(1) << 50
		eib = int64(1) << 60
	)
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{kib, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048575, "1.0 MiB"}, // 1 MiB − 1: one decimal rounds it up to the next unit, never "1024.0 KiB"
		{mib - 52, "1023.9 KiB"},
		{mib, "1.0 MiB"},
		{20_447_232, "19.5 MiB"},
		{gib, "1.0 GiB"},
		{3 * gib, "3.0 GiB"},
		{tib, "1.0 TiB"},
		{pib, "1.0 PiB"},
		{eib, "1.0 EiB"},
		{math.MaxInt64, "8.0 EiB"},
		{-1, "-1 B"},
		{-1023, "-1023 B"},
		{-1536, "-1.5 KiB"},
		{math.MinInt64, "-8.0 EiB"},
	} {
		if got := model.FormatBytes(tc.in); got != tc.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFormatByteAmount is the rule for amounts that need not be whole (charts, rates).
func TestFormatByteAmount(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{0, "0 B"},
		{0.25, "0 B"},
		{-0.25, "0 B"},
		{1023.4, "1023 B"},
		{1023.5, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024*1024 - 0.5, "1.0 MiB"},
		{200_000_000, "190.7 MiB"},
		{math.Pow(1024, 7), "1024.0 EiB"}, // beyond EiB it stays in EiB
	} {
		if got := model.FormatByteAmount(tc.in); got != tc.want {
			t.Errorf("FormatByteAmount(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := model.FormatByteRate(1536); got != "1.5 KiB/s" {
		t.Errorf("FormatByteRate(1536) = %q", got)
	}
}

// TestFormatByteQuantity converts quantities as objects write them; what is no whole number of
// bytes stays as written.
func TestFormatByteQuantity(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// The owner's Node drawer.
		{"16360780Ki", "15.6 GiB"},
		{"119842348Ki", "114.3 GiB"},
		{"13856852378", "12.9 GiB"},
		{"119400000000", "111.2 GiB"},
		{"10Gi", "10.0 GiB"},
		{"1.5Gi", "1.5 GiB"},
		{"128Mi", "128.0 MiB"},
		{"512", "512 B"},
		{"0", "0 B"},
		// Decimal suffixes convert as well: the place to talk about (D-32).
		{"1G", "953.7 MiB"},
		{"500M", "476.8 MiB"},
		{"1k", "1000 B"},
		{"1e3", "1000 B"},
		// As written: milli-bytes, fractions of a byte, no quantity, beyond what a quantity holds.
		{"500m", "500m"},
		{"2000m", "2000m"},
		{"1.5", "1.5"},
		{"1.0001Ki", "1.0001Ki"},
		{"", ""},
		{"lots", "lots"},
		{"1Xi", "1Xi"},
		{"10Ei", "10Ei"},
		{"1e999", "1e999"},
	} {
		if got := model.FormatByteQuantity(tc.in); got != tc.want {
			t.Errorf("FormatByteQuantity(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestByteResource names the resources whose quantities are bytes.
func TestByteResource(t *testing.T) {
	for _, name := range []string{
		"memory", "storage", "ephemeral-storage", "hugepages-2Mi", "hugepages-1Gi",
		"requests.memory", "limits.memory", "requests.storage", "requests.ephemeral-storage",
		"limits.ephemeral-storage", "requests.hugepages-2Mi", "gold.storageclass.storage.k8s.io/requests.storage",
	} {
		if !model.ByteResource(name) {
			t.Errorf("%s is bytes", name)
		}
	}
	for _, name := range []string{
		"cpu", "requests.cpu", "limits.cpu", "pods", "count/persistentvolumeclaims", "persistentvolumeclaims",
		"nvidia.com/gpu", "example.com/memory", "gold.storageclass.storage.k8s.io/persistentvolumeclaims", "services",
	} {
		if model.ByteResource(name) {
			t.Errorf("%s is not bytes", name)
		}
	}
	if got := model.FormatResource("memory", "128Mi"); got != "128.0 MiB" {
		t.Errorf("memory 128Mi: %q", got)
	}
	if got := model.FormatResource("cpu", "250m"); got != "250m" {
		t.Errorf("cpu 250m: %q", got)
	}
}
