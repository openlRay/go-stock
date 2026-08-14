package agent

import "testing"

func TestCalculateNextRunTimesUsesChinaStandardTime(t *testing.T) {
	times := NewCronTaskApi().CalculateNextRunTimes("0 0 12 * * *", 1)
	if len(times) != 1 {
		t.Fatalf("CalculateNextRunTimes() returned %d values, want 1", len(times))
	}
	if got := times[0].Location().String(); got != cronTaskTimezone {
		t.Fatalf("next run location = %q, want %q", got, cronTaskTimezone)
	}
}
