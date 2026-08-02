package server

import (
	"path/filepath"
	"testing"
)

func TestDeviceTelemetryStorePersistsAndPrunes(t *testing.T) {
	store := NewDeviceTelemetryStore(filepath.Join(t.TempDir(), "device.jsonl"), 2)
	for _, temperature := range []float64{41.5, 43.0, 42.0} {
		if err := store.Append(DeviceTelemetrySample{
			ChipTemperatureC: temperature,
			TemperatureKnown: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	samples, err := store.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].ChipTemperatureC != 42.0 || samples[1].ChipTemperatureC != 43.0 {
		t.Fatalf("samples = %#v", samples)
	}
	summary := summarizeDeviceTelemetry(samples)
	if summary.CurrentC != 42.0 || summary.MinimumC != 42.0 || summary.MaximumC != 43.0 {
		t.Fatalf("summary = %#v", summary)
	}
}
