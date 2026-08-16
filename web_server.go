//go:build web

package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"go-stock/backend/agent"
	"go-stock/backend/data"
)

//go:embed frontend/wailsjs/go/main/App.js
var webBindingSource embed.FS

// webDesktopOnlyMethods 只能由桌面 Wails 运行时执行，或由 web-bridge 使用浏览器原生
// 能力替代。它们不进入 Web RPC 表，避免直接 HTTP 请求触发窗口、退出、更新和文件
// 对话框能力。
var webDesktopOnlyMethods = map[string]struct{}{
	"CheckUpdate":                   {},
	"ExportConfig":                  {},
	"ImportSkillPackage":            {},
	"ImportTradingRecordsFromExcel": {},
	"OpenURL":                       {},
	"PickKBFilePath":                {},
	"PickKBFilePaths":               {},
	"QuitApp":                       {},
	"RestartAsAdmin":                {},
	"SaveAsMarkdown":                {},
	"SaveImage":                     {},
	"SaveWordFile":                  {},
	"UploadKBFile":                  {},
	"UploadKBFiles":                 {},
}

const (
	webTradingImportMaxSize = int64(20 << 20)
	webKBFileMaxSize        = int64(10 << 20)
	webKBBatchMaxSize       = int64(100 << 20)
	webKBBatchMaxFiles      = 20
	webMultipartMemory      = int64(8 << 20)
	webMultipartOverhead    = int64(1 << 20)
)

var (
	webTradingImportExtensions = map[string]struct{}{
		".csv": {}, ".txt": {}, ".xls": {}, ".xlsx": {},
	}
	webKBFileExtensions = map[string]struct{}{
		".md": {}, ".markdown": {}, ".txt": {},
	}
)

type webRPCRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type webRPCResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

type webUploadSpec struct {
	fieldName         string
	description       string
	tempPrefix        string
	maxFiles          int
	maxFileSize       int64
	maxTotalFileSize  int64
	allowedExtensions map[string]struct{}
}

type webUploadFailure struct {
	status  int
	message string
}

type webUploadedFiles struct {
	tempDir string
	paths   []string
}

func (u *webUploadedFiles) cleanup() {
	if u != nil && u.tempDir != "" {
		_ = os.RemoveAll(u.tempDir)
		u.tempDir = ""
	}
}

type webEvent struct {
	Name string `json:"name"`
	Data []any  `json:"data"`
}

type webEventHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
	done    chan struct{}
	close   sync.Once
}

func newWebEventHub() *webEventHub {
	return &webEventHub{
		clients: make(map[chan []byte]struct{}),
		done:    make(chan struct{}),
	}
}

func (h *webEventHub) Emit(name string, data ...any) {
	payload, err := json.Marshal(webEvent{Name: name, Data: data})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client <- payload:
		default:
			// 单个浏览器消费过慢时不阻塞行情和 AI 后台任务。
		}
	}
}

func (h *webEventHub) Close() {
	h.close.Do(func() {
		close(h.done)
	})
}

func (h *webEventHub) subscribe() chan []byte {
	client := make(chan []byte, 2048)
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()
	return client
}

func (h *webEventHub) unsubscribe(client chan []byte) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
}

type webAPI struct {
	app                  *App
	hub                  *webEventHub
	allowedMethods       map[string]struct{}
	staticFS             fs.FS
	importTradingRecords func(string) (*data.TradingRecordImportResult, error)
	uploadKBFile         func(string, string) ([]string, error)
	uploadKBFiles        func(string, []string, func()) error
}

func newWebHTTPServer(addr string, app *App, hub *webEventHub) (*http.Server, error) {
	staticFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return nil, fmt.Errorf("加载前端静态资源失败: %w", err)
	}
	allowedMethods, err := loadWebBindingMethods()
	if err != nil {
		return nil, err
	}
	api := &webAPI{
		app:            app,
		hub:            hub,
		allowedMethods: allowedMethods,
		staticFS:       staticFS,
		uploadKBFile:   agent.AddUploadedFileToKB,
		uploadKBFiles:  agent.StartBatchImportWithCleanup,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", api.health)
	mux.HandleFunc("/api/rpc", api.rpc)
	mux.HandleFunc("/api/rpc/", api.rpc)
	mux.HandleFunc("/api/events", api.events)
	mux.HandleFunc("/api/skills/import", api.importSkill)
	mux.HandleFunc("/api/trading-records/import", api.importTradingRecordFile)
	mux.HandleFunc("/api/knowledge-base/file/import", api.importKBFile)
	mux.HandleFunc("/api/knowledge-base/files/import", api.importKBFiles)
	mux.HandleFunc("/", api.serveSPA)

	server := &http.Server{
		Addr:              addr,
		Handler:           webSecurityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	server.RegisterOnShutdown(hub.Close)
	return server, nil
}

func (a *webAPI) importSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !hasWebMediaType(r, "multipart/form-data") {
		writeWebJSON(w, http.StatusUnsupportedMediaType, webRPCResponse{Error: "技能包上传必须使用 multipart/form-data"})
		return
	}

	const maxPackageSize = 100 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxPackageSize+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "读取技能包失败: " + err.Error()})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "请选择 ZIP 技能包"})
		return
	}
	defer file.Close()
	if !strings.EqualFold(filepath.Ext(header.Filename), ".zip") {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "仅支持 ZIP 技能包"})
		return
	}

	tempFile, err := os.CreateTemp("", "go-stock-skill-*.zip")
	if err != nil {
		writeWebJSON(w, http.StatusInternalServerError, webRPCResponse{Error: "创建临时文件失败: " + err.Error()})
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	written, copyErr := io.Copy(tempFile, io.LimitReader(file, maxPackageSize+1))
	closeErr := tempFile.Close()
	if copyErr != nil || closeErr != nil {
		if copyErr == nil {
			copyErr = closeErr
		}
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "保存技能包失败: " + copyErr.Error()})
		return
	}
	if written > maxPackageSize {
		writeWebJSON(w, http.StatusRequestEntityTooLarge, webRPCResponse{Error: "技能包超过 100MB 限制"})
		return
	}

	writeWebJSON(w, http.StatusOK, webRPCResponse{Result: a.app.importSkillPackage(tempPath, header.Filename)})
}

func (a *webAPI) importTradingRecordFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	uploaded, failure := receiveWebUploadedFiles(w, r, webUploadSpec{
		fieldName:         "file",
		description:       "交易记录文件",
		tempPrefix:        "go-stock-trading-import-*",
		maxFiles:          1,
		maxFileSize:       webTradingImportMaxSize,
		maxTotalFileSize:  webTradingImportMaxSize,
		allowedExtensions: webTradingImportExtensions,
	})
	if failure != nil {
		writeWebJSON(w, failure.status, webRPCResponse{Error: failure.message})
		return
	}
	defer uploaded.cleanup()

	importer := a.importTradingRecords
	if importer == nil {
		importer = data.NewStockDataApi().ImportTradingRecords
	}
	result, err := importer(uploaded.paths[0])
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "导入交易记录失败: " + sanitizeWebUploadError(err, uploaded.tempDir)})
		return
	}
	writeWebJSON(w, http.StatusOK, webRPCResponse{Result: result})
}

func (a *webAPI) importKBFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	uploaded, failure := receiveWebUploadedFiles(w, r, webUploadSpec{
		fieldName:         "file",
		description:       "知识库文件",
		tempPrefix:        "go-stock-kb-file-*",
		maxFiles:          1,
		maxFileSize:       webKBFileMaxSize,
		maxTotalFileSize:  webKBFileMaxSize,
		allowedExtensions: webKBFileExtensions,
	})
	if failure != nil {
		writeWebJSON(w, failure.status, webRPCResponse{Error: failure.message})
		return
	}
	defer uploaded.cleanup()

	kbName := webMultipartValue(r, "kbName")
	if kbName == "" {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "知识库名称不能为空"})
		return
	}
	uploader := a.uploadKBFile
	if uploader == nil {
		uploader = agent.AddUploadedFileToKB
	}
	docIDs, err := uploader(kbName, uploaded.paths[0])
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "导入知识库文件失败: " + sanitizeWebUploadError(err, uploaded.tempDir)})
		return
	}
	writeWebJSON(w, http.StatusOK, webRPCResponse{Result: docIDs})
}

func (a *webAPI) importKBFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	uploaded, failure := receiveWebUploadedFiles(w, r, webUploadSpec{
		fieldName:         "files",
		description:       "知识库文件",
		tempPrefix:        "go-stock-kb-files-*",
		maxFiles:          webKBBatchMaxFiles,
		maxFileSize:       webKBFileMaxSize,
		maxTotalFileSize:  webKBBatchMaxSize,
		allowedExtensions: webKBFileExtensions,
	})
	if failure != nil {
		writeWebJSON(w, failure.status, webRPCResponse{Error: failure.message})
		return
	}

	kbName := webMultipartValue(r, "kbName")
	if kbName == "" {
		uploaded.cleanup()
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "知识库名称不能为空"})
		return
	}
	uploader := a.uploadKBFiles
	if uploader == nil {
		uploader = agent.StartBatchImportWithCleanup
	}
	if err := uploader(kbName, uploaded.paths, uploaded.cleanup); err != nil {
		message := sanitizeWebUploadError(err, uploaded.tempDir)
		uploaded.cleanup()
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "启动知识库文件导入失败: " + message})
		return
	}
	writeWebJSON(w, http.StatusOK, webRPCResponse{Result: nil})
}

func receiveWebUploadedFiles(w http.ResponseWriter, r *http.Request, spec webUploadSpec) (*webUploadedFiles, *webUploadFailure) {
	if !hasWebMediaType(r, "multipart/form-data") {
		return nil, &webUploadFailure{status: http.StatusUnsupportedMediaType, message: spec.description + "上传必须使用 multipart/form-data"}
	}
	if spec.maxFiles <= 0 || spec.maxFileSize <= 0 || spec.maxTotalFileSize <= 0 {
		return nil, &webUploadFailure{status: http.StatusInternalServerError, message: "上传配置无效"}
	}

	r.Body = http.MaxBytesReader(w, r.Body, spec.maxTotalFileSize+webMultipartOverhead)
	if err := r.ParseMultipartForm(webMultipartMemory); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, &webUploadFailure{status: http.StatusRequestEntityTooLarge, message: spec.description + "上传总大小超过限制"}
		}
		return nil, &webUploadFailure{status: http.StatusBadRequest, message: "读取" + spec.description + "失败"}
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if r.MultipartForm == nil {
		return nil, &webUploadFailure{status: http.StatusBadRequest, message: "读取" + spec.description + "失败"}
	}

	headers := r.MultipartForm.File[spec.fieldName]
	if len(headers) == 0 {
		return nil, &webUploadFailure{status: http.StatusBadRequest, message: "请选择" + spec.description}
	}
	if len(headers) > spec.maxFiles {
		return nil, &webUploadFailure{status: http.StatusBadRequest, message: fmt.Sprintf("%s数量不能超过 %d 个", spec.description, spec.maxFiles)}
	}

	tempDir, err := os.MkdirTemp("", spec.tempPrefix)
	if err != nil {
		return nil, &webUploadFailure{status: http.StatusInternalServerError, message: "创建上传临时目录失败"}
	}
	uploaded := &webUploadedFiles{tempDir: tempDir, paths: make([]string, 0, len(headers))}
	totalSize := int64(0)
	for index, header := range headers {
		fileName := sanitizeWebUploadFileName(header.Filename)
		if fileName == "" {
			uploaded.cleanup()
			return nil, &webUploadFailure{status: http.StatusBadRequest, message: spec.description + "名称无效"}
		}
		extension := strings.ToLower(filepath.Ext(fileName))
		if _, ok := spec.allowedExtensions[extension]; !ok {
			uploaded.cleanup()
			return nil, &webUploadFailure{status: http.StatusBadRequest, message: fmt.Sprintf("不支持 %s 文件", extension)}
		}

		fileDir := filepath.Join(tempDir, fmt.Sprintf("%02d", index))
		if err := os.Mkdir(fileDir, 0o700); err != nil {
			uploaded.cleanup()
			return nil, &webUploadFailure{status: http.StatusInternalServerError, message: "创建上传临时目录失败"}
		}
		filePath := filepath.Join(fileDir, fileName)
		written, err := saveWebUploadPart(header, filePath, spec.maxFileSize)
		if err != nil {
			uploaded.cleanup()
			return nil, &webUploadFailure{status: http.StatusBadRequest, message: "保存" + spec.description + "失败"}
		}
		if written > spec.maxFileSize {
			uploaded.cleanup()
			return nil, &webUploadFailure{status: http.StatusRequestEntityTooLarge, message: fmt.Sprintf("%s %q 超过单文件大小限制", spec.description, fileName)}
		}
		totalSize += written
		if totalSize > spec.maxTotalFileSize {
			uploaded.cleanup()
			return nil, &webUploadFailure{status: http.StatusRequestEntityTooLarge, message: spec.description + "上传总大小超过限制"}
		}
		uploaded.paths = append(uploaded.paths, filePath)
	}
	return uploaded, nil
}

func saveWebUploadPart(header *multipart.FileHeader, filePath string, maxSize int64) (int64, error) {
	source, err := header.Open()
	if err != nil {
		return 0, err
	}
	defer source.Close()

	target, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(target, io.LimitReader(source, maxSize+1))
	closeErr := target.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	return written, nil
}

func sanitizeWebUploadFileName(fileName string) string {
	fileName = filepath.Base(strings.ReplaceAll(strings.TrimSpace(fileName), "\\", "/"))
	if fileName == "" || fileName == "." {
		return ""
	}
	return fileName
}

func sanitizeWebUploadError(err error, tempDir string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if tempDir != "" {
		message = strings.ReplaceAll(message, tempDir, "上传文件")
	}
	return message
}

func webMultipartValue(r *http.Request, name string) string {
	if r.MultipartForm == nil {
		return ""
	}
	values := r.MultipartForm.Value[name]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func loadWebBindingMethods() (map[string]struct{}, error) {
	source, err := webBindingSource.ReadFile("frontend/wailsjs/go/main/App.js")
	if err != nil {
		return nil, fmt.Errorf("读取 Wails binding 失败: %w", err)
	}
	re := regexp.MustCompile(`(?m)^export function ([A-Za-z][A-Za-z0-9_]*)\(`)
	matches := re.FindAllSubmatch(source, -1)
	if len(matches) == 0 {
		return nil, errors.New("Wails binding 中没有可用方法")
	}
	methods := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		name := string(match[1])
		if _, desktopOnly := webDesktopOnlyMethods[name]; desktopOnly {
			continue
		}
		methods[name] = struct{}{}
	}
	return methods, nil
}

func (a *webAPI) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"mode":    "web",
		"version": Version,
		"time":    time.Now().Format(time.RFC3339),
	})
}

func (a *webAPI) rpc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !hasWebMediaType(r, "application/json") {
		writeWebJSON(w, http.StatusUnsupportedMediaType, webRPCResponse{Error: "RPC 请求必须使用 application/json"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	var req webRPCRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "请求格式错误: " + err.Error()})
		return
	}
	methodName := req.Method
	if strings.HasPrefix(r.URL.Path, "/api/rpc/") {
		methodName = strings.TrimPrefix(r.URL.Path, "/api/rpc/")
		if req.Method != "" && req.Method != methodName {
			writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "RPC 路径与请求方法不一致"})
			return
		}
	}
	if _, ok := a.allowedMethods[methodName]; !ok {
		writeWebJSON(w, http.StatusNotFound, webRPCResponse{Error: "未知方法: " + methodName})
		return
	}
	result, err := callWebMethod(a.app, methodName, req.Args)
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: err.Error()})
		return
	}
	writeWebJSON(w, http.StatusOK, webRPCResponse{Result: result})
}

func callWebMethod(app *App, methodName string, rawArgs []json.RawMessage) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("调用 %s 失败: %v", methodName, recovered)
		}
	}()

	method := reflect.ValueOf(app).MethodByName(methodName)
	if !method.IsValid() {
		return nil, fmt.Errorf("方法未实现: %s", methodName)
	}
	methodType := method.Type()
	if methodType.IsVariadic() {
		return nil, fmt.Errorf("暂不支持可变参数方法: %s", methodName)
	}
	if len(rawArgs) != methodType.NumIn() {
		return nil, fmt.Errorf("%s 参数数量错误: 需要 %d 个，收到 %d 个", methodName, methodType.NumIn(), len(rawArgs))
	}

	args := make([]reflect.Value, methodType.NumIn())
	for i := 0; i < methodType.NumIn(); i++ {
		argType := methodType.In(i)
		argHolder := reflect.New(argType)
		if err := json.Unmarshal(rawArgs[i], argHolder.Interface()); err != nil {
			return nil, fmt.Errorf("%s 第 %d 个参数无效: %w", methodName, i+1, err)
		}
		args[i] = argHolder.Elem()
	}

	outputs := method.Call(args)
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if len(outputs) > 0 && outputs[len(outputs)-1].Type().Implements(errorType) {
		errorValue := outputs[len(outputs)-1]
		outputs = outputs[:len(outputs)-1]
		if !errorValue.IsNil() {
			return nil, errorValue.Interface().(error)
		}
	}

	switch len(outputs) {
	case 0:
		return nil, nil
	case 1:
		return outputs[0].Interface(), nil
	default:
		values := make([]any, len(outputs))
		for i, output := range outputs {
			values[i] = output.Interface()
		}
		return values, nil
	}
}

func (a *webAPI) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeWebJSON(w, http.StatusInternalServerError, webRPCResponse{Error: "当前 HTTP Server 不支持事件流"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	client := a.hub.subscribe()
	defer a.hub.unsubscribe(client)
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()
	for {
		select {
		case <-a.hub.done:
			return
		case <-r.Context().Done():
			return
		case payload := <-client:
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		}
	}
}

func (a *webAPI) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	if info, err := fs.Stat(a.staticFS, name); err == nil && !info.IsDir() {
		http.FileServer(http.FS(a.staticFS)).ServeHTTP(w, r)
		return
	}
	index, err := fs.ReadFile(a.staticFS, "index.html")
	if err != nil {
		http.Error(w, "前端资源不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}

func writeWebJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func webSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		if !isSameOriginWebRequest(r) {
			writeWebJSON(w, http.StatusForbidden, webRPCResponse{Error: "拒绝跨站请求"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hasWebMediaType(r *http.Request, expected string) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, expected)
}

func isSameOriginWebRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}
