package model

import (
	"math"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// Byte amounts as Cogpit shows them (D-32, owner): whole bytes up to 1023 B, from 1024 bytes in
// KiB, from 1024 KiB in MiB, and so on, each with one decimal. Cogpit's defaults: a space between
// number and unit as IEC 80000-13 writes it, the units B, KiB, MiB, GiB, TiB, PiB and EiB, and a
// decimal point whatever the region (the UI is English; D-31's dates follow the region, numbers do
// not). Every byte amount a person or an agent reads as drawn goes through these functions;
// structured results keep their numbers.

// byteUnits are the units from KiB up; an amount beyond 1023.9 EiB stays in EiB.
var byteUnits = [...]string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// FormatBytes formats a byte count: "0 B", "1023 B", "1.0 KiB", "19.5 MiB", "15.6 GiB". An amount
// that one decimal would round up to 1024 of a unit is written in the next one: 1 MiB less one
// byte is "1.0 MiB", never "1024.0 KiB".
func FormatBytes(b int64) string {
	if b > -1024 && b < 1024 {
		return strconv.FormatInt(b, 10) + " B"
	}
	return FormatByteAmount(float64(b))
}

// FormatByteAmount is FormatBytes for an amount that need not be whole, a chart's value or a
// rate averaged over a window: below 1024 B it is rounded to whole bytes ("0 B" for 0.25).
func FormatByteAmount(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'g', -1, 64) + " B"
	}
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	if r := math.Round(v); r < 1024 {
		if r == 0 {
			sign = "" // no "-0 B"
		}
		return sign + strconv.FormatFloat(r, 'f', 0, 64) + " B"
	}
	n, i := v/1024, 0
	text := strconv.FormatFloat(n, 'f', 1, 64)
	// The unit is chosen by the text as written, so that rounding never shows 1024.0 of a unit.
	for i < len(byteUnits)-1 {
		if shown, _ := strconv.ParseFloat(text, 64); shown < 1024 {
			break
		}
		n /= 1024
		i++
		text = strconv.FormatFloat(n, 'f', 1, 64)
	}
	return sign + text + " " + byteUnits[i]
}

// FormatByteRate formats bytes per second: "512 B/s", "1.5 KiB/s", "190.7 MiB/s".
func FormatByteRate(v float64) string { return FormatByteAmount(v) + "/s" }

// FormatByteQuantity formats a Kubernetes quantity of bytes as an object writes it with
// FormatBytes: "16360780Ki" is "15.6 GiB", "13856852378" "12.9 GiB", "10Gi" "10.0 GiB", and a
// decimal suffix converts too: "1G" is "953.7 MiB", "500M" "476.8 MiB". What is no whole number of
// bytes is returned as it is written: milli-bytes ("500m", "2000m": a quantity in milli-units is
// a mistake the user should see as written), a fraction of a byte ("1.5"), text that is no
// quantity, and a quantity beyond what Kubernetes holds (it clamps "10Ei" to 8 EiB).
func FormatByteQuantity(s string) string {
	if strings.HasSuffix(s, "m") {
		return s
	}
	q, ok := ParseQuantity(s)
	if !ok {
		return s
	}
	v := q.Value() // rounded up to a whole byte
	if v == math.MaxInt64 || v == math.MinInt64 || q.Cmp(*resource.NewQuantity(v, resource.BinarySI)) != 0 {
		return s
	}
	return FormatBytes(v)
}

// ByteResource reports whether a resource's quantities are bytes: memory, storage,
// ephemeral-storage and huge pages ("hugepages-2Mi"), also as a resource quota names them
// ("requests.memory", "limits.ephemeral-storage", "requests.hugepages-1Gi",
// "gold.storageclass.storage.k8s.io/requests.storage"). CPU, counts ("pods",
// "count/persistentvolumeclaims") and extended resources ("nvidia.com/gpu") are not.
func ByteResource(name string) bool {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		if !strings.HasSuffix(name[:i], ".storageclass.storage.k8s.io") {
			return false
		}
		name = name[i+1:]
	}
	if rest, ok := strings.CutPrefix(name, "requests."); ok {
		name = rest
	} else if rest, ok := strings.CutPrefix(name, "limits."); ok {
		name = rest
	}
	switch name {
	case "memory", "storage", "ephemeral-storage":
		return true
	}
	return strings.HasPrefix(name, "hugepages-")
}

// FormatResource formats one quantity of a resource list: a byte resource's with
// FormatByteQuantity ("memory", "128Mi": "128.0 MiB"), any other as it is written ("cpu",
// "250m"; "pods", "110").
func FormatResource(name, quantity string) string {
	if ByteResource(name) {
		return FormatByteQuantity(quantity)
	}
	return quantity
}
