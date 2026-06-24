package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/core"
)

// 日志事件订阅（前端通过 App 转发到 Wails 事件总线消费）
var (
	logSubs []chan string
	subMu   sync.Mutex
)

// SubscribeLogs 订阅日志事件，返回带缓冲的接收通道
func SubscribeLogs() chan string {
	ch := make(chan string, 20)
	subMu.Lock()
	logSubs = append(logSubs, ch)
	subMu.Unlock()
	return ch
}

// UnsubscribeLogs 退订并关闭通道
func UnsubscribeLogs(ch chan string) {
	subMu.Lock()
	for i, c := range logSubs {
		if c == ch {
			logSubs = append(logSubs[:i], logSubs[i+1:]...)
			break
		}
	}
	subMu.Unlock()
	close(ch)
}

func broadcastLog(msg string) {
	subMu.Lock()
	subs := make([]chan string, len(logSubs))
	copy(subs, logSubs)
	subMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

func addLog(msg string) {
	broadcastLog(msg)
}

type Client struct {
	mu      sync.Mutex
	running bool
	httpLn  net.Listener
	socksLn net.Listener
	cipher  core.Cipher
	ssAddr  string
}

func New() *Client { return &Client{} }

func (c *Client) Running() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.running }

func (c *Client) Start(server string, port int, password, method string, httpPort, socksPort int) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("already running")
	}
	ssAddr := fmt.Sprintf("%s:%d", server, port)

	ciph, err := core.PickCipher(method, nil, password)
	if err != nil {
		c.mu.Unlock()
		return err
	}

	httpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", httpPort))
	if err != nil {
		c.mu.Unlock()
		return err
	}

	socksLn, err := net.Listen("tcp", fmt.Sprintf(":%d", socksPort))
	if err != nil {
		httpLn.Close()
		c.mu.Unlock()
		return err
	}

	c.running = true
	c.httpLn = httpLn
	c.socksLn = socksLn
	c.cipher = ciph
	c.ssAddr = ssAddr
	c.mu.Unlock()

	msg := fmt.Sprintf("[代理] 已启动 HTTP :%d SOCKS5 :%d → %s (%s)", httpPort, socksPort, ssAddr, method)
	log.Println(msg)
	addLog(msg)

	SetSystemProxy(httpPort)
	addLog(fmt.Sprintf("[系统] 代理已设置 127.0.0.1:%d", httpPort))

	go c.serveHTTP()
	go c.serveSOCKS()
	return nil
}

func (c *Client) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	httpLn := c.httpLn
	socksLn := c.socksLn
	c.mu.Unlock()

	RestoreSystemProxy()
	addLog("[系统] 代理已恢复")

	if httpLn != nil {
		httpLn.Close()
	}
	if socksLn != nil {
		socksLn.Close()
	}
	log.Println("[代理] 已停止")
	addLog("[代理] 已停止")
}

func (c *Client) serveHTTP() {
	for {
		c.mu.Lock()
		ln := c.httpLn
		c.mu.Unlock()
		if ln == nil {
			return
		}
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go c.handle(conn)
	}
}

func (c *Client) serveSOCKS() {
	for {
		c.mu.Lock()
		ln := c.socksLn
		c.mu.Unlock()
		if ln == nil {
			return
		}
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go c.handleSOCKS(conn)
	}
}

func (c *Client) handleSOCKS(conn net.Conn) {
	defer conn.Close()

	target, err := socks5Handshake(conn)
	if err != nil {
		return
	}

	// 国内/局域网目标直连，不走 SS 加密
	if host, port, e := parseSSTarget(target); e == nil && shouldBypass(host) {
		log.Printf("[直连] SOCKS5 %s:%s (国内/局域网)", host, port)
		addLog(fmt.Sprintf("[直连] %s:%s (国内/局域网)", host, port))
		directRelay(conn, net.JoinHostPort(host, port))
		return
	}

	c.mu.Lock()
	cipher := c.cipher
	ssAddr := c.ssAddr
	c.mu.Unlock()

	remote, err := net.DialTimeout("tcp", ssAddr, 10*time.Second)
	if err != nil {
		log.Printf("[代理] 连接 %s 失败: %v", ssAddr, err)
		return
	}
	defer remote.Close()

	remote = cipher.StreamConn(remote)
	if _, err = remote.Write(target); err != nil {
		return
	}

	if host, port, e := parseSSTarget(target); e == nil {
		log.Printf("[代理] SOCKS5 %s:%s", host, port)
		addLog(fmt.Sprintf("[代理] %s:%s", host, port))
	}

	relay(conn, remote)
}

// socks5Handshake 读取 SOCKS5 握手，返回 SS 目标地址格式
func socks5Handshake(conn net.Conn) ([]byte, error) {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	buf := make([]byte, 256)
	n, err := io.ReadFull(conn, buf[:2])
	if err != nil || n < 2 || buf[0] != 5 {
		return nil, fmt.Errorf("非 SOCKS5 ver=%d", buf[0])
	}
	nmethods := int(buf[1])
	if nmethods > 0 {
		if _, err = io.ReadFull(conn, buf[:nmethods]); err != nil {
			return nil, err
		}
	}
	conn.Write([]byte{5, 0})

	n, err = io.ReadFull(conn, buf[:4])
	if err != nil || buf[1] != 1 {
		return nil, fmt.Errorf("非 CONNECT")
	}

	tgt := []byte{buf[0], buf[1], 0, buf[3]}
	switch buf[3] {
	case 1:
		io.ReadFull(conn, buf[:6])
		tgt = append(tgt, buf[:6]...)
	case 3:
		io.ReadFull(conn, buf[:1])
		l := int(buf[0])
		tgt = append(tgt, buf[0])
		rest := make([]byte, l+2)
		io.ReadFull(conn, rest)
		tgt = append(tgt, rest...)
	case 4:
		rest := make([]byte, 18)
		io.ReadFull(conn, rest)
		tgt = append(tgt, rest...)
	}

	conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	return tgt, nil
}

func (c *Client) handle(client net.Conn) {
	defer client.Close()

	// 读取首个 HTTP 请求包（保留原始字节，供普通 HTTP 请求原样转发）
	first, err := readHTTPRequest(client)
	if err != nil || len(first) == 0 {
		client.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	// 判断请求类型：CONNECT 隧道  vs  普通 HTTP 绝对 URI
	method, host, port, isConnect, rest, parseErr := parseHTTPHead(first)
	if parseErr != nil {
		client.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		log.Printf("[代理] 解析请求失败: %v", parseErr)
		return
	}

	// 国内/局域网目标直连，不走 SS 加密
	if shouldBypass(host) {
		log.Printf("[直连] HTTP %s %s:%s (国内/局域网)", method, host, port)
		addLog(fmt.Sprintf("[直连] %s %s:%s (国内/局域网)", method, host, port))
		directRelayHTTP(client, net.JoinHostPort(host, port), isConnect, rest)
		return
	}

	c.mu.Lock()
	cipher := c.cipher
	ssAddr := c.ssAddr
	c.mu.Unlock()

	// Go 直连 SS 服务器
	remote, err := net.DialTimeout("tcp", ssAddr, 10*time.Second)
	if err != nil {
		log.Printf("[代理] 连接 %s 失败: %v", ssAddr, err)
		msg := fmt.Sprintf("[代理] 连接 SS 失败: %v", err)
		addLog(msg)
		client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer remote.Close()

	// SS 加密
	remote = cipher.StreamConn(remote)

	// 发送目标地址
	hostPort := net.JoinHostPort(host, port)
	ssTgt := buildSSTargetDomain(hostPort)
	if _, err = remote.Write(ssTgt); err != nil {
		return
	}

	log.Printf("[代理] HTTP %s %s:%s", method, host, port)
	addLog(fmt.Sprintf("[代理] %s %s:%s", method, host, port))

	// CONNECT: 回 200 后双向透明转发；
	// 普通 HTTP: 把首包（原始请求）写到 SS 通道
	if isConnect {
		client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	} else {
		remote.Write(rest)
	}

	// 双向转发
	relay(client, remote)
}

// readHTTPRequest 读取到 \r\n\r\n 结束的完整 HTTP 请求头，返回原始字节
// 在调用方设置的超时内读取；若已读到的数据包含结尾则提前返回
func readHTTPRequest(conn net.Conn) ([]byte, error) {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	buf := bytes.Buffer{}
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			// 头部结束于 \r\n\r\n
			if idx := bytes.Index(buf.Bytes(), []byte("\r\n\r\n")); idx >= 0 {
				// 包括可能的 body（首段），全部返回
				return buf.Bytes(), nil
			}
		}
		if err != nil {
			if buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, err
		}
		if buf.Len() > 64*1024 {
			return buf.Bytes(), nil
		}
	}
}

// parseHTTPHead 解析 HTTP 请求首字节
// 返回: method, host, port, isConnect, 原始头字节(含可能 body), err
//
// 支持两类请求：
//  1. CONNECT host:port HTTP/1.1   (HTTPS 隧道)
//  2. GET http://host[:port]/...   (普通 HTTP 代理，绝对 URI)
//
// 注意：对非 CONNECT 的普通 HTTP 请求，返回的 rest = 完整原始字节（含请求行），
// 直连时按需重写请求行（绝对 URI → 相对 URI）；经 SS 代理时也按此处理。
func parseHTTPHead(data []byte) (method, host, port string, isConnect bool, rest []byte, err error) {
	rest = data
	// 提取首行（请求行）
	lineEnd := bytes.IndexByte(data, '\n')
	if lineEnd < 0 {
		err = fmt.Errorf("非法请求（无换行）")
		return
	}
	line := strings.TrimRight(string(data[:lineEnd]), "\r")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		err = fmt.Errorf("非法请求行: %q", line)
		return
	}
	method = parts[0]
	target := parts[1]
	_ = parts[2]

	if strings.EqualFold(method, "CONNECT") {
		// CONNECT host:port
		isConnect = true
		h, p, e := net.SplitHostPort(target)
		if e == nil {
			host, port = h, p
		} else {
			host = target
			port = "443"
		}
		return
	}

	// 普通 HTTP: GET http://host[:port]/path
	u, e := url.Parse(target)
	if e != nil || u.Host == "" {
		err = fmt.Errorf("非绝对 URI 请求: %q", target)
		return
	}
	host = u.Hostname()
	port = u.Port()
	if port == "" {
		port = "80"
	}
	// 重写请求行：绝对 URI → 相对路径（Origin Form），便于后端识别
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	newReqLine := method + " " + path + " " + parts[2]
	// 关键：必须保留 CRLF 行结束符。data[lineEnd] 是 '\n'，其前一字节通常是 '\r'。
	// 这里跳过原 '\n'，自己补上 "\r\n"，避免请求行末尾变成单 LF 导致严谨的
	// 服务器（路由器/NAS 等简陋 HTTP 服务）回 400 Bad Request。
	afterLine := data[lineEnd+1:] // 跳过原 '\n'
	rest = append([]byte(newReqLine+"\r\n"), afterLine...)
	// 去掉 hop-by-hop 头，避免后端困惑
	rest = stripHopByHop(rest)
	return
}

// stripHopByHop 去除逐跳头（Proxy-Connection / Connection 等）。
// 按原始字节逐行扫描重建，保留 CRLF 与请求体，大小写不敏感匹配头名。
func stripHopByHop(b []byte) []byte {
	hop := map[string]struct{}{
		"proxy-connection":    {},
		"proxy-authorization": {},
		"connection":          {},
		"keep-alive":          {},
		"te":                  {},
		"trailer":             {},
		"transfer-encoding":   {},
		"upgrade":             {},
	}
	var out bytes.Buffer
	// 按 \n 切分，每行若尾部是 \r 则保留
	lines := bytes.Split(b, []byte("\n"))
	for i, line := range lines {
		// 提取本行头名（首个 ':' 之前），去空白与小写化
		trim := bytes.TrimSpace(line)
		colon := bytes.IndexByte(trim, ':')
		drop := false
		if colon > 0 {
			name := strings.ToLower(string(trim[:colon]))
			if _, ok := hop[name]; ok {
				drop = true
			}
		} else if len(trim) == 0 && i == len(lines)-1 {
			// 末尾空行（即末尾 \r\n\r\n 的最后段），保留
		}
		if !drop {
			out.Write(line)
			if i < len(lines)-1 {
				out.WriteByte('\n')
			}
		}
	}
	return out.Bytes()
}

// buildSSTargetDomain 由 host:port 构造 SS 域名型目标地址
//
//	[3][len][domain][port 大端 2 字节]
func buildSSTargetDomain(hostPort string) []byte {
	h, p, err := net.SplitHostPort(hostPort)
	if err != nil {
		h = hostPort
	}
	port := 443
	fmt.Sscanf(p, "%d", &port)
	// 若是 IPv4 字面量，使用 type=1，精度更高
	if ip := net.ParseIP(h); ip != nil && ip.To4() != nil {
		ip4 := ip.To4()
		tgt := []byte{1}
		tgt = append(tgt, ip4...)
		tgt = append(tgt, byte(port>>8), byte(port&0xff))
		return tgt
	}
	tgt := []byte{3, byte(len(h))}
	tgt = append(tgt, []byte(h)...)
	tgt = append(tgt, byte(port>>8), byte(port&0xff))
	return tgt
}
