package main

import (
	"errors"
	"go-stock/backend/agent"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type cronTaskEventRecorder struct {
	t      *testing.T
	events []models.CronTaskExecutedEvent
}

func (r *cronTaskEventRecorder) Emit(name string, args ...any) {
	if name != models.CronTaskExecutedEventName {
		return
	}
	if len(args) != 1 {
		r.t.Errorf("cron event args = %d", len(args))
		return
	}
	event, ok := args[0].(models.CronTaskExecutedEvent)
	if !ok {
		r.t.Errorf("cron event type = %T", args[0])
		return
	}
	var stored models.CronTask
	if err := db.Dao.First(&stored, event.TaskID).Error; err != nil {
		r.t.Errorf("cron event emitted before task could be read: %v", err)
		return
	}
	if stored.RunCount == 0 || strings.TrimSpace(stored.LastRunResult) == "" {
		r.t.Errorf("cron event emitted before run info persisted: %+v", stored)
		return
	}
	if event.CompletedAt.IsZero() {
		r.t.Error("cron event completedAt is zero")
		return
	}
	r.events = append(r.events, event)
}

func TestExecuteCronTaskNotificationBoundary(t *testing.T) {
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&models.CronTask{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })

	api := agent.NewCronTaskApi()
	pushCount := 0
	var pushedResult *agent.CronTaskExecutionResult
	eventRecorder := &cronTaskEventRecorder{t: t}
	app := &App{
		eventEmitter: eventRecorder,
		cronResultPusher: func(_ *models.CronTask, result *agent.CronTaskExecutionResult) error {
			pushCount++
			pushedResult = result
			return errors.New("simulated notification failure")
		},
	}

	off := &models.CronTask{Name: "关闭通知", CronExpr: "0 0 * * * *", TaskType: "custom", Enable: true, Status: "active"}
	if err := api.Create(off); err != nil {
		t.Fatalf("create off task: %v", err)
	}
	if err := app.executeCronTask(off); err != nil {
		t.Fatalf("execute off task: %v", err)
	}
	if pushCount != 0 {
		t.Fatalf("notification-off task pushed %d times", pushCount)
	}

	on := &models.CronTask{Name: "开启通知", CronExpr: "0 0 * * * *", TaskType: "custom", Enable: true, Status: "active", NotifyOnCompletion: true}
	if err := api.Create(on); err != nil {
		t.Fatalf("create on task: %v", err)
	}
	if err := app.executeCronTask(on); err != nil {
		t.Fatalf("notification failure must not change successful task result: %v", err)
	}
	if pushCount != 1 || pushedResult == nil || !pushedResult.Success {
		t.Fatalf("successful task push = %d, %+v", pushCount, pushedResult)
	}
	stored, err := api.GetByID(on.ID)
	if err != nil {
		t.Fatalf("read successful task: %v", err)
	}
	if stored.RunCount != 1 || !strings.HasPrefix(stored.LastRunResult, "成功:") {
		t.Fatalf("successful run info = %+v", stored)
	}

	failed := &models.CronTask{Name: "失败通知", CronExpr: "0 0 * * * *", TaskType: "unknown", Enable: true, Status: "active", NotifyOnCompletion: true}
	if err := api.Create(failed); err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	if err := app.executeCronTask(failed); err == nil {
		t.Fatal("unknown task should return its business execution error")
	}
	if pushCount != 2 || pushedResult == nil || pushedResult.Success {
		t.Fatalf("failed task push = %d, %+v", pushCount, pushedResult)
	}
	stored, err = api.GetByID(failed.ID)
	if err != nil {
		t.Fatalf("read failed task: %v", err)
	}
	if stored.RunCount != 1 || !strings.HasPrefix(stored.LastRunResult, "失败:") {
		t.Fatalf("failed run info = %+v", stored)
	}
	if len(eventRecorder.events) != 3 {
		t.Fatalf("cron completion events = %+v", eventRecorder.events)
	}
	if !eventRecorder.events[0].Success || !eventRecorder.events[1].Success || eventRecorder.events[2].Success {
		t.Fatalf("cron completion event statuses = %+v", eventRecorder.events)
	}
}

func TestExecuteCronTaskStrategyScreeningUsesUnifiedNotificationBoundary(t *testing.T) {
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&models.CronTask{}, &models.CustomStrategy{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })

	api := agent.NewCronTaskApi()
	task := &models.CronTask{
		Name: "策略推送", CronExpr: "0 0 9 * * *", TaskType: agent.CronTaskTypeStrategyScreening,
		Params: `{"strategyId":999,"pushLimit":20}`, Enable: true, Status: "active", NotifyOnCompletion: false,
	}
	if err := api.Create(task); err != nil {
		t.Fatalf("create strategy task: %v", err)
	}
	if !task.NotifyOnCompletion {
		t.Fatal("strategy screening task should force completion notification")
	}

	pushCount := 0
	var pushedResult *agent.CronTaskExecutionResult
	eventRecorder := &cronTaskEventRecorder{t: t}
	app := &App{
		eventEmitter: eventRecorder,
		cronResultPusher: func(_ *models.CronTask, result *agent.CronTaskExecutionResult) error {
			pushCount++
			pushedResult = result
			return nil
		},
	}

	if err := app.executeCronTask(task); err == nil {
		t.Fatal("deleted strategy should fail execution")
	}
	if pushCount != 1 || pushedResult == nil || pushedResult.Success {
		t.Fatalf("strategy failure push = %d, %+v", pushCount, pushedResult)
	}
	if !strings.Contains(pushedResult.Summary, "所选策略不存在或已删除") {
		t.Fatalf("strategy failure summary = %q", pushedResult.Summary)
	}
	if len(eventRecorder.events) != 1 || eventRecorder.events[0].Success {
		t.Fatalf("strategy completion events = %+v", eventRecorder.events)
	}
}

func TestExecuteCronTaskSkipsCompletionSideEffectsWhenRunInfoPersistenceFails(t *testing.T) {
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&models.CronTask{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })

	task := &models.CronTask{
		Name: "持久化失败", CronExpr: "0 0 * * * *", TaskType: "custom",
		Enable: true, Status: "active", NotifyOnCompletion: true,
	}
	if err := agent.NewCronTaskApi().Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := database.Migrator().DropTable(&models.CronTask{}); err != nil {
		t.Fatalf("drop cron task table: %v", err)
	}

	pushCount := 0
	eventRecorder := &cronTaskEventRecorder{t: t}
	app := &App{
		eventEmitter: eventRecorder,
		cronResultPusher: func(*models.CronTask, *agent.CronTaskExecutionResult) error {
			pushCount++
			return nil
		},
	}
	if err := app.executeCronTask(task); err == nil || !strings.Contains(err.Error(), "更新任务运行信息失败") {
		t.Fatalf("executeCronTask() error = %v", err)
	}
	if len(eventRecorder.events) != 0 || pushCount != 0 {
		t.Fatalf("persistence failure side effects: events=%d pushes=%d", len(eventRecorder.events), pushCount)
	}
}

func TestCronTaskScheduleRegistryUsesStableID(t *testing.T) {
	previous := db.Dao
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := database.AutoMigrate(&models.CronTask{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.Dao = database
	t.Cleanup(func() { db.Dao = previous })

	appCron := newAppCron()
	appCron.Start()
	app := &App{cron: appCron, cronEntrys: make(map[string]cron.EntryID)}
	t.Cleanup(func() { app.cron.Stop() })
	task := &models.CronTask{
		Name: "原任务名", CronExpr: "0 0 23 * * *", TaskType: "custom",
		Enable: true, Status: "active", NotifyOnCompletion: true,
	}
	if result := app.CreateCronTask(task); result != "创建成功" {
		t.Fatalf("CreateCronTask() = %q", result)
	}
	firstEntry, exists := app.getCronEntry(cronTaskEntryKey(task.ID))
	if !exists || len(app.cron.Entries()) != 1 {
		t.Fatalf("created schedule = %v, entries=%d", exists, len(app.cron.Entries()))
	}

	task.Name = "修改后的任务名"
	if result := app.UpdateCronTask(task); result != "更新成功" {
		t.Fatalf("UpdateCronTask() = %q", result)
	}
	updatedEntry, exists := app.getCronEntry(cronTaskEntryKey(task.ID))
	if !exists || updatedEntry == firstEntry || len(app.cron.Entries()) != 1 {
		t.Fatalf("renamed schedule should replace old entry: first=%d updated=%d exists=%v entries=%d",
			firstEntry, updatedEntry, exists, len(app.cron.Entries()))
	}

	if result := app.EnableCronTask(task.ID, false); result != "操作成功" {
		t.Fatalf("EnableCronTask(false) = %q", result)
	}
	if _, exists := app.getCronEntry(cronTaskEntryKey(task.ID)); exists || len(app.cron.Entries()) != 0 {
		t.Fatalf("disabled task left scheduler state: exists=%v entries=%d", exists, len(app.cron.Entries()))
	}
	if result := app.EnableCronTask(task.ID, true); result != "操作成功" {
		t.Fatalf("EnableCronTask(true) = %q", result)
	}
	if _, exists := app.getCronEntry(cronTaskEntryKey(task.ID)); !exists || len(app.cron.Entries()) != 1 {
		t.Fatalf("re-enabled task should have one schedule: exists=%v entries=%d", exists, len(app.cron.Entries()))
	}

	if result := app.DeleteCronTask(task.ID); result != "删除成功" {
		t.Fatalf("DeleteCronTask() = %q", result)
	}
	if _, exists := app.getCronEntry(cronTaskEntryKey(task.ID)); exists || len(app.cron.Entries()) != 0 {
		t.Fatalf("deleted task left scheduler state: exists=%v entries=%d", exists, len(app.cron.Entries()))
	}

	disabled := &models.CronTask{
		Name: "停用任务", CronExpr: "0 0 23 * * *", TaskType: "custom",
		Enable: false, Status: "paused",
	}
	if result := app.CreateCronTask(disabled); result != "创建成功" {
		t.Fatalf("CreateCronTask(disabled) = %q", result)
	}
	if _, exists := app.getCronEntry(cronTaskEntryKey(disabled.ID)); exists || len(app.cron.Entries()) != 0 {
		t.Fatalf("disabled task should not be scheduled: exists=%v entries=%d", exists, len(app.cron.Entries()))
	}
}
