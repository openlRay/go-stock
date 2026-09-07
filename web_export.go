//go:build web

package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"go-stock/backend/data"
)

const webTableExportMaxSize = int64(16 << 20)
const webXLSXMediaType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// exportTable 只接收表格内容并返回下载字节，不接受或写入服务器文件路径。
func (a *webAPI) exportTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !hasWebMediaType(r, "application/json") {
		writeWebJSON(w, http.StatusUnsupportedMediaType, webRPCResponse{Error: "仅支持 JSON 表格数据"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, webTableExportMaxSize)
	var request struct {
		Filename string               `json:"filename"`
		Table    data.ExportTableData `json:"table"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	if err == nil {
		var extra any
		if nextErr := decoder.Decode(&extra); nextErr != io.EOF {
			err = nextErr
			if err == nil {
				err = errors.New("multiple JSON values")
			}
		}
	}
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			writeWebJSON(w, http.StatusRequestEntityTooLarge, webRPCResponse{Error: "导出数据不能超过 16 MB"})
		} else {
			writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "无效的表格数据"})
		}
		return
	}
	content, err := (data.StockDataApi{}).BuildTableXLSX(request.Table)
	if err != nil {
		writeWebJSON(w, http.StatusBadRequest, webRPCResponse{Error: "无法导出，请检查表格名称、列定义和数据量"})
		return
	}
	filename := path.Base(strings.ReplaceAll(strings.TrimSpace(request.Filename), "\\", "/"))
	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, filename)
	if filename == "" || filename == "." || filename == "/" {
		filename = "导出数据.xlsx"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".xlsx") {
		filename += ".xlsx"
	}
	writeWebXLSX(w, filename, content)
}

func writeWebXLSX(w http.ResponseWriter, filename string, content []byte) {
	w.Header().Set("Content-Type", webXLSXMediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	// 自定义 header 必须保持 ASCII；浏览器 Fetch 会按字节读取未编码的中文而产生乱码。
	w.Header().Set("X-Download-Filename", url.PathEscape(filename))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(content)
}
