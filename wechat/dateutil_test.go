package wechat

import (
	"testing"
	"time"
)

func TestFormatDate(t *testing.T) {
	loc := ShanghaiLoc()
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{"normal date", time.Date(2026, 3, 18, 10, 0, 0, 0, loc), "2026-03-18"},
		{"new year", time.Date(2026, 1, 1, 0, 0, 0, 0, loc), "2026-01-01"},
		{"end of year", time.Date(2026, 12, 31, 23, 59, 59, 0, loc), "2026-12-31"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDate(tt.time)
			if got != tt.want {
				t.Errorf("FormatDate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid date", "2026-03-18", false},
		{"invalid format", "03-18-2026", true},
		{"empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestAddDays(t *testing.T) {
	tests := []struct {
		name    string
		date    string
		days    int
		want    string
		wantErr bool
	}{
		{"add 1 day", "2026-03-18", 1, "2026-03-19", false},
		{"subtract 1 day", "2026-03-18", -1, "2026-03-17", false},
		{"cross month boundary", "2026-01-30", 3, "2026-02-02", false},
		{"subtract across month", "2026-03-01", -1, "2026-02-28", false},
		{"add 0 days", "2026-03-18", 0, "2026-03-18", false},
		{"cross year boundary", "2025-12-30", 5, "2026-01-04", false},
		{"invalid date", "bad-date", 1, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AddDays(tt.date, tt.days)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddDays(%q, %d) error = %v, wantErr %v", tt.date, tt.days, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("AddDays(%q, %d) = %q, want %q", tt.date, tt.days, got, tt.want)
			}
		})
	}
}

func TestGetDateRange(t *testing.T) {
	tests := []struct {
		name      string
		begin     string
		end       string
		wantCount int
		wantErr   bool
	}{
		{"single day", "2026-03-18", "2026-03-18", 1, false},
		{"3 days", "2026-03-16", "2026-03-18", 3, false},
		{"cross month", "2026-01-30", "2026-02-02", 4, false},
		{"end before begin", "2026-03-20", "2026-03-18", 0, false},
		{"invalid begin", "bad", "2026-03-18", 0, true},
		{"invalid end", "2026-03-18", "bad", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetDateRange(tt.begin, tt.end)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDateRange error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) != tt.wantCount {
				t.Errorf("GetDateRange len = %d, want %d, got %v", len(got), tt.wantCount, got)
			}
		})
	}
}

func TestYesterday(t *testing.T) {
	y := Yesterday()
	parsed, err := ParseDate(y)
	if err != nil {
		t.Fatalf("Yesterday() returned unparseable date: %q", y)
	}

	now := time.Now().In(ShanghaiLoc())
	expected := now.AddDate(0, 0, -1)

	if parsed.Year() != expected.Year() || parsed.Month() != expected.Month() || parsed.Day() != expected.Day() {
		t.Errorf("Yesterday() = %q, expected %s", y, FormatDate(expected))
	}
}
