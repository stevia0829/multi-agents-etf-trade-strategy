package main

import (
	"testing"
	"time"
)

func TestValidateClosePreviewTime(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	tests := []struct {
		name    string
		hour    int
		minute  int
		wantErr bool
	}{
		{name: "before window", hour: 14, minute: 44, wantErr: true},
		{name: "recommended time", hour: 14, minute: 50},
		{name: "after window", hour: 14, minute: 57, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 18, tt.hour, tt.minute, 0, 0, loc)
			err := validateClosePreviewTime(now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateClosePreviewTime() error=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
