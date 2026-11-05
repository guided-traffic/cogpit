package model_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

func TestUsageFormats(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{model.FormatCPU(0), "0"},
		{model.FormatCPU(1), "1m"},
		{model.FormatCPU(12), "12m"},
		{model.FormatCPU(999), "999m"},
		{model.FormatCPU(1000), "1"},
		{model.FormatCPU(1004), "1"},
		{model.FormatCPU(1005), "1.01"},
		{model.FormatCPU(1500), "1.5"},
		{model.FormatCPU(1994), "1.99"},
		{model.FormatCPU(1995), "2"},
		{model.FormatCPU(2275), "2.28"},
		{model.FormatCPU(10570), "10.57"},
		{model.FormatCPU(146500), "146.5"},
		{model.FormatCPU(39000), "39"},
		{model.FormatCPU(-250), "-250m"},
		{model.FormatCPU(-1500), "-1.5"},
		{model.FormatBytes(0), "0 B"},
		{model.FormatBytes(1023), "1023 B"},
		{model.FormatBytes(1024), "1.0 KiB"},
		{model.FormatBytes(20_447_232), "19.5 MiB"},
		{model.FormatBytes(3 << 30), "3.0 GiB"},
		{model.FormatPercent(0.075), "7.5%"},
		{model.FormatPercent(0.424), "42%"},
		{model.FormatPercent(1.2), "120%"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestPodUsageTotal(t *testing.T) {
	p := model.PodUsage{Containers: []model.ContainerUsage{
		{Name: "a", Usage: model.Usage{CPUMilli: 10, MemoryBytes: 100}},
		{Name: "b", Usage: model.Usage{CPUMilli: 5, MemoryBytes: 1}},
	}}
	if got := p.Total(); got != (model.Usage{CPUMilli: 15, MemoryBytes: 101}) {
		t.Errorf("total = %+v", got)
	}
}

// The usage bars' colours at the thresholds (Cogpit's defaults: warning from 80%, error from 90%)
// and the cell a share makes: its text, sort key and bar.
func TestUsageBars(t *testing.T) {
	for _, tc := range []struct {
		share float64
		want  model.Status
	}{
		{0, model.StatusNone}, {0.5, model.StatusNone}, {0.7999, model.StatusNone},
		{0.8, model.StatusWarning}, {0.8999, model.StatusWarning},
		{0.9, model.StatusError}, {1.2, model.StatusError},
	} {
		if got := model.UsageBarStatus(tc.share); got != tc.want {
			t.Errorf("UsageBarStatus(%v) = %q, want %q", tc.share, got, tc.want)
		}
	}
	c := model.ShareCell(0.875, "7.0 GiB of 8.0 GiB")
	if c.Text != "88%" || c.Num != 875 || c.Bar == nil || c.Bar.Share != 0.875 || c.Bar.Status != model.StatusWarning || c.Bar.Detail != "7.0 GiB of 8.0 GiB" {
		t.Errorf("ShareCell(0.875) = %+v, bar %+v", c, c.Bar)
	}
	if c := model.ShareCell(-0.1, ""); c.Text != "0.0%" || c.Num != 0 || c.Bar.Share != 0 {
		t.Errorf("a negative share = %+v, want 0", c)
	}
}
