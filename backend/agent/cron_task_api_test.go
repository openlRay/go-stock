package agent

import (
	"context"
	"errors"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func withCronTaskTestDB(t *testing.T) {
	t.Helper()
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&models.CronTask{}, &models.Motto{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })
}

func TestCronTaskMigrationDefaultsNotificationOff(t *testing.T) {
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })

	if err := db.Dao.Exec(`CREATE TABLE cron_tasks (
		id integer PRIMARY KEY AUTOINCREMENT,
		name text NOT NULL,
		cron_expr text NOT NULL,
		task_type text NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create legacy cron table: %v", err)
	}
	if err := db.Dao.Exec(`INSERT INTO cron_tasks (name, cron_expr, task_type) VALUES ('旧任务', '0 0 * * * *', 'custom')`).Error; err != nil {
		t.Fatalf("insert legacy cron task: %v", err)
	}
	if err := db.Dao.AutoMigrate(&models.CronTask{}); err != nil {
		t.Fatalf("migrate cron task: %v", err)
	}
	var task models.CronTask
	if err := db.Dao.First(&task).Error; err != nil {
		t.Fatalf("read migrated cron task: %v", err)
	}
	if task.NotifyOnCompletion {
		t.Fatal("legacy task notification should default to false")
	}
}

func TestCronTaskUpdatePersistsFalseAndMottoForcesTrue(t *testing.T) {
	withCronTaskTestDB(t)
	api := NewCronTaskApi()
	disabled := &models.CronTask{
		Name: "停用任务", CronExpr: "0 0 * * * *", TaskType: "custom",
		Enable: false, Status: "paused",
	}
	if err := api.Create(disabled); err != nil {
		t.Fatalf("create disabled task: %v", err)
	}
	storedDisabled, err := api.GetByID(disabled.ID)
	if err != nil {
		t.Fatalf("read disabled task: %v", err)
	}
	if disabled.Enable || storedDisabled.Enable {
		t.Fatalf("Create should persist explicit false enable: input=%v stored=%v", disabled.Enable, storedDisabled.Enable)
	}

	regular := &models.CronTask{
		Name: "普通任务", CronExpr: "0 0 * * * *", TaskType: "custom", Enable: true,
		Status: "active", NotifyOnCompletion: true,
	}
	if err := api.Create(regular); err != nil {
		t.Fatalf("create regular task: %v", err)
	}
	regular.NotifyOnCompletion = false
	if err := api.Update(regular); err != nil {
		t.Fatalf("update regular task: %v", err)
	}
	stored, err := api.GetByID(regular.ID)
	if err != nil {
		t.Fatalf("read regular task: %v", err)
	}
	if stored.NotifyOnCompletion {
		t.Fatal("Update should persist false notifyOnCompletion")
	}

	motto := &models.CronTask{
		Name: "每日格言", CronExpr: "0 0 9 * * *", TaskType: CronTaskTypeMottoPush,
		Enable: true, Status: "active", NotifyOnCompletion: false,
	}
	if err := api.Create(motto); err != nil {
		t.Fatalf("create motto task: %v", err)
	}
	if !motto.NotifyOnCompletion {
		t.Fatal("motto task Create should force notification on")
	}
	motto.NotifyOnCompletion = false
	if err := api.Update(motto); err != nil {
		t.Fatalf("update motto task: %v", err)
	}
	stored, err = api.GetByID(motto.ID)
	if err != nil {
		t.Fatalf("read motto task: %v", err)
	}
	if !stored.NotifyOnCompletion {
		t.Fatal("motto task Update should force notification on")
	}
}

func TestExecuteMottoTaskFailureAndRandomThreeResult(t *testing.T) {
	withCronTaskTestDB(t)
	api := NewCronTaskApi()
	task := &models.CronTask{
		Name: "每日格言", CronExpr: "0 0 9 * * *", TaskType: CronTaskTypeMottoPush,
		Enable: true, Status: "active",
	}
	if err := api.Create(task); err != nil {
		t.Fatalf("create motto task: %v", err)
	}

	failed, err := api.ExecuteTask(context.Background(), task)
	if err == nil || failed == nil || failed.Success {
		t.Fatalf("ExecuteTask(empty) = %+v, %v", failed, err)
	}
	if !strings.Contains(failed.LastRunResult, "格言库为空") {
		t.Fatalf("empty result = %q", failed.LastRunResult)
	}

	for _, content := range []string{"格言一", "格言二", "格言三", "格言四"} {
		if err := db.Dao.Create(&models.Motto{Content: content}).Error; err != nil {
			t.Fatalf("seed motto: %v", err)
		}
	}
	result, err := api.ExecuteTask(context.Background(), task)
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if !result.Success || result.Summary != "已随机选取 3 条格言" {
		t.Fatalf("ExecuteTask() result = %+v", result)
	}
	lines := strings.Split(result.PlainText, "\n")
	if len(lines) != 4 {
		t.Fatalf("PlainText = %q, want title plus 3 mottos", result.PlainText)
	}
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		content := strings.TrimSpace(strings.SplitN(line, ". ", 2)[1])
		if seen[content] {
			t.Fatalf("duplicate motto in result: %q", content)
		}
		seen[content] = true
	}

	stored, err := api.GetByID(task.ID)
	if err != nil {
		t.Fatalf("read task after runs: %v", err)
	}
	if stored.RunCount != 2 || !strings.HasPrefix(stored.LastRunResult, "成功:") {
		t.Fatalf("stored run info = %+v", stored)
	}
}

func TestCronTaskFailureSummaryDoesNotExposeDependencyError(t *testing.T) {
	dependencyErr := errors.New("provider body contains sk-secret and private reasoning")
	if got := cronTaskFailureSummary(dependencyErr); got != "任务执行失败，请稍后重试" {
		t.Fatalf("cronTaskFailureSummary() = %q", got)
	}

	publicErr := newCronTaskPublicError("格言库为空，请先添加格言", dependencyErr)
	if got := cronTaskFailureSummary(publicErr); got != "格言库为空，请先添加格言" {
		t.Fatalf("public cronTaskFailureSummary() = %q", got)
	}
	if !errors.Is(publicErr, dependencyErr) {
		t.Fatal("public cron error should preserve its cause")
	}
}
