package agent

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadedKBFileMetadataDoesNotExposeTemporaryPath(t *testing.T) {
	tempPath := filepath.Join(t.TempDir(), "report.md")
	metadata := buildKBFileMetadata(tempPath, 123, false)
	if _, ok := metadata["file_path"]; ok {
		t.Fatalf("uploaded metadata exposed temporary path: %v", metadata)
	}
	if metadata["file_size"] != "123" {
		t.Fatalf("file_size = %q", metadata["file_size"])
	}
}

func TestAddUploadedFilesToKBDoesNotExposeTemporaryPath(t *testing.T) {
	kbName := "uploaded-result-path-test"
	tempPath := filepath.Join(t.TempDir(), "missing.md")
	t.Cleanup(func() {
		kbVectorizingMu.Lock()
		delete(kbVectorizingStatuses, kbName)
		kbVectorizingMu.Unlock()
	})

	summary, err := AddUploadedFilesToKB(kbName, []string{tempPath})
	if err != nil {
		t.Fatalf("AddUploadedFilesToKB() error = %v", err)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("results = %+v", summary.Results)
	}
	result := summary.Results[0]
	if result.FilePath != "" {
		t.Fatalf("FilePath = %q", result.FilePath)
	}
	if strings.Contains(result.Error, filepath.Dir(tempPath)) {
		t.Fatalf("Error exposed temporary directory: %q", result.Error)
	}

	status := GetKBVectorizingStatus(kbName)
	if status == nil || len(status.Results) != 1 || status.Results[0].FilePath != "" {
		t.Fatalf("status = %+v", status)
	}
}

func TestLaunchBatchImportCleansUpAfterRunnerFinishes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	cleaned := make(chan struct{})

	launchBatchImport("cleanup-test", []string{"demo.md"}, func() {
		close(cleaned)
	}, func(_ string, _ []string) (*KBBatchImportSummary, error) {
		close(started)
		<-release
		return &KBBatchImportSummary{}, nil
	})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("后台导入未启动")
	}
	select {
	case <-cleaned:
		t.Fatal("runner 完成前不应清理临时文件")
	default:
	}

	close(release)
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("runner 完成后未执行 cleanup")
	}
}

func TestLaunchBatchImportCleansUpAfterPanic(t *testing.T) {
	kbName := "cleanup-panic-test"
	cleaned := make(chan struct{})
	t.Cleanup(func() {
		kbVectorizingMu.Lock()
		delete(kbVectorizingStatuses, kbName)
		kbVectorizingMu.Unlock()
	})

	launchBatchImport(kbName, []string{"demo.md"}, func() {
		close(cleaned)
	}, func(_ string, _ []string) (*KBBatchImportSummary, error) {
		panic("/tmp/go-stock-kb-files-secret/report.md")
	})

	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("panic 后未执行 cleanup")
	}

	status := GetKBVectorizingStatus(kbName)
	if status == nil || status.Error != "内部错误" {
		t.Fatalf("status = %+v", status)
	}
}
