package main

import "testing"

func TestNewAppUsesChinaStandardTimeForCron(t *testing.T) {
	scheduler := newAppCron()

	if got := scheduler.Location().String(); got != "Asia/Shanghai" {
		t.Fatalf("cron location = %q, want Asia/Shanghai", got)
	}
}
