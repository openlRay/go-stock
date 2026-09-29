package agent

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-stock/backend/data"
)

func TestVisionImageDownloadHTTPBoundaries(t *testing.T) {
	redirected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("image-bytes"))
		case "/redirect":
			http.Redirect(w, r, "/private", http.StatusFound)
		case "/private":
			redirected = true
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	// 将已验证的公网测试地址映射到本地 server，测试期间不访问真实网络。
	transport := data.CreateHTTPClientWithTimeout(time.Second).GetClient().Transport.(*http.Transport)
	originalDial, originalProxy := transport.DialContext, transport.Proxy
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "8.8.8.8:80" {
			t.Errorf("没有连接已验证的地址: %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	t.Cleanup(func() { transport.DialContext, transport.Proxy = originalDial, originalProxy })
	body, mime, err := downloadVisionImage("http://8.8.8.8/image")
	if err != nil || string(body) != "image-bytes" || mime != "image/png" {
		t.Fatalf("图片下载失败: %q %q %v", body, mime, err)
	}
	for _, path := range []string{"/redirect", "/missing"} {
		if _, _, err := downloadVisionImage("http://8.8.8.8" + path); err == nil {
			t.Fatalf("未拒绝 %s 响应", path)
		}
	}
	if redirected {
		t.Fatal("不应跟随图片重定向")
	}
}

func TestVisionImageDownloadRejectsPrivateTargets(t *testing.T) {
	for _, host := range []string{"localhost", "x.localhost", "127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "::1", "fc00::1"} {
		if _, err := resolveVisionImageHost(context.Background(), host); err == nil {
			t.Errorf("应拒绝图片地址 %s", host)
		}
	}
	if !publicVisionImageIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("公网 IP 被错误拒绝")
	}
	for _, address := range []string{"file:///etc/passwd", "https://user:secret@example.com/image.png", "http://127.0.0.1/image.png"} {
		if _, _, err := downloadVisionImage(address); err == nil {
			t.Errorf("应在请求前拒绝 %s", address)
		}
	}
}

func TestVisionImageDownloadSizeLimit(t *testing.T) {
	for _, size := range []int{0, 1, maxVisionImageDownloadSize, maxVisionImageDownloadSize + 20} {
		reader := bytes.NewReader(make([]byte, size))
		body, err := readVisionImageBody(reader)
		valid := size > 0 && size <= maxVisionImageDownloadSize
		if (err == nil) != valid {
			t.Fatalf("size=%d error=%v", size, err)
		}
		if valid && len(body) != size {
			t.Fatalf("图片读取不完整: %d", len(body))
		}
		if size > maxVisionImageDownloadSize && reader.Len() != 19 {
			t.Fatal("超限响应未及时停止读取")
		}
	}
}
