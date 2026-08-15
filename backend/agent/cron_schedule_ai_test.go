package agent

import (
	"reflect"
	"testing"
)

func TestNormalizeCronSchedule(t *testing.T) {
	tests := []struct {
		name        string
		input       cronScheduleAIOutput
		wantCron    string
		wantSummary string
	}{
		{
			name:        "five minute interval",
			input:       cronScheduleAIOutput{Mode: "interval", IntervalValue: 5, IntervalUnit: "minute"},
			wantCron:    "0 */5 * * * *",
			wantSummary: "每 5 分钟执行一次",
		},
		{
			name:        "three day interval",
			input:       cronScheduleAIOutput{Mode: "interval", IntervalValue: 3, IntervalUnit: "day"},
			wantCron:    "0 0 0 */3 * *",
			wantSummary: "每 3 天执行一次",
		},
		{
			name:        "daily noon",
			input:       cronScheduleAIOutput{Mode: "daily", Time: "12:00"},
			wantCron:    "0 0 12 * * *",
			wantSummary: "每天 12:00 执行一次",
		},
		{
			name:        "weekday afternoon",
			input:       cronScheduleAIOutput{Mode: "weekly", Time: "15:00", Weekdays: []int{5, 1, 3, 2, 4, 1}},
			wantCron:    "0 0 15 * * 1,2,3,4,5",
			wantSummary: "每周一、周二、周三、周四、周五 15:00 执行一次",
		},
		{
			name:        "monthly",
			input:       cronScheduleAIOutput{Mode: "monthly", Time: "09:30", MonthDay: 15},
			wantCron:    "0 30 9 15 * *",
			wantSummary: "每月 15 日 09:30 执行一次",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := normalizeCronSchedule(test.input)
			if err != nil {
				t.Fatalf("normalizeCronSchedule() error = %v", err)
			}
			if result.CronExpr != test.wantCron {
				t.Fatalf("CronExpr = %q, want %q", result.CronExpr, test.wantCron)
			}
			if result.Summary != test.wantSummary {
				t.Fatalf("Summary = %q, want %q", result.Summary, test.wantSummary)
			}
			if result.Timezone != cronScheduleTimezone {
				t.Fatalf("Timezone = %q, want %q", result.Timezone, cronScheduleTimezone)
			}
		})
	}
}

func TestNormalizeCronScheduleRejectsInvalidIntent(t *testing.T) {
	tests := []cronScheduleAIOutput{
		{Mode: "interval", IntervalValue: 0, IntervalUnit: "minute"},
		{Mode: "interval", IntervalValue: 24, IntervalUnit: "hour"},
		{Mode: "interval", IntervalValue: 32, IntervalUnit: "day"},
		{Mode: "daily", Time: "25:00"},
		{Mode: "weekly", Time: "09:00", Weekdays: nil},
		{Mode: "weekly", Time: "09:00", Weekdays: []int{7}},
		{Mode: "monthly", Time: "09:00", MonthDay: 32},
		{Mode: "custom"},
	}

	for _, input := range tests {
		if _, err := normalizeCronSchedule(input); err == nil {
			t.Fatalf("normalizeCronSchedule(%+v) expected an error", input)
		}
	}
}

func TestNormalizeWeekdaysDeduplicatesAndSorts(t *testing.T) {
	got, err := normalizeWeekdays([]int{5, 1, 0, 5, 3})
	if err != nil {
		t.Fatalf("normalizeWeekdays() error = %v", err)
	}
	want := []int{0, 1, 3, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeWeekdays() = %v, want %v", got, want)
	}
}

func TestExtractJSONObject(t *testing.T) {
	input := "```json\n{\"mode\":\"daily\",\"time\":\"12:00\"}\n```"
	want := `{"mode":"daily","time":"12:00"}`
	if got := extractJSONObject(input); got != want {
		t.Fatalf("extractJSONObject() = %q, want %q", got, want)
	}
}
