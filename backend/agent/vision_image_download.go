package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go-stock/backend/data"
)

func publicVisionImageIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func resolveVisionImageHost(ctx context.Context, host string) ([]net.IPAddr, error) {
	if host == "" || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return nil, fmt.Errorf("图片地址必须指向公网")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("无法解析图片地址")
	}
	for _, ip := range ips {
		if !publicVisionImageIP(ip.IP) {
			return nil, fmt.Errorf("图片地址必须指向公网")
		}
	}
	return ips, nil
}

// downloadVisionImage 保留应用代理与超时配置；公网地址校验、禁止重定向和限量读取
// 共同约束 Web 用户可提交的图片 URL，防止服务器访问内网或无界读取响应。
func downloadVisionImage(rawURL string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
		return nil, "", fmt.Errorf("图片地址必须是无凭据的 HTTP(S) 链接")
	}
	ips, err := resolveVisionImageHost(ctx, u.Hostname())
	if err != nil {
		return nil, "", err
	}
	client := data.CreateHTTPClientWithTimeout(60 * time.Second).GetClient()
	transport := client.Transport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		// 直连时固定已验证的 IP，避免校验与连接之间发生 DNS 重绑定。
		// 使用用户配置的代理时保留代理连接地址，目标仍须通过前面的公网校验。
		if strings.EqualFold(host, u.Hostname()) {
			addr = net.JoinHostPort(ips[0].IP.String(), port)
		}
		return dial(ctx, network, addr)
	}
	client.Transport = transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("下载图片失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("下载图片失败: HTTP %d", resp.StatusCode)
	}
	body, err := readVisionImageBody(resp.Body)
	return body, resp.Header.Get("Content-Type"), err
}

func readVisionImageBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxVisionImageDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取图片失败")
	}
	if len(body) == 0 || len(body) > maxVisionImageDownloadSize {
		return nil, fmt.Errorf("图片为空或超过 10MB 限制")
	}
	return body, nil
}
