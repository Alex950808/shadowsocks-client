package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// parseSSTarget 从 Shadowsocks 目标地址字节解析出 host / port
// 格式: [1字节 type][type 对应地址][2字节 port(大端)]
//   - type 1: 4 字节 IPv4
//   - type 3: 1 字节长度 + 域名
//   - type 4: 16 字节 IPv6
func parseSSTarget(t []byte) (host, port string, err error) {
	if len(t) < 1 {
		return "", "", fmt.Errorf("空目标地址")
	}
	typ := t[0]
	body := t[1:]
	switch typ {
	case 1: // IPv4
		if len(body) < 6 {
			return "", "", fmt.Errorf("IPv4 地址长度不足")
		}
		ip := net.IPv4(body[0], body[1], body[2], body[3])
		port = fmt.Sprintf("%d", uint16(body[4])<<8|uint16(body[5]))
		host = ip.String()
	case 4: // IPv6
		if len(body) < 18 {
			return "", "", fmt.Errorf("IPv6 地址长度不足")
		}
		ip := make(net.IP, 16)
		copy(ip, body[:16])
		port = fmt.Sprintf("%d", uint16(body[16])<<8|uint16(body[17]))
		host = ip.String()
	case 3: // 域名
		if len(body) < 1 {
			return "", "", fmt.Errorf("域名地址长度不足")
		}
		l := int(body[0])
		if len(body) < 1+l+2 {
			return "", "", fmt.Errorf("域名地址长度不足")
		}
		host = string(body[1 : 1+l])
		port = fmt.Sprintf("%d", uint16(body[1+l])<<8|uint16(body[2+l]))
	default:
		return "", "", fmt.Errorf("未知目标地址类型 %d", typ)
	}
	return host, port, nil
}

// shouldBypass 判断目标是否应直连（不走 SS 代理）
// 判定顺序：
//  1. LAN/本机 (loopback / 私有 / .local / 无点主机名) → 直连
//  2. 国内域名后缀 (.cn / .com.cn 等) → 直连
//  3. IP 字面量或域名解析后的 IP 命中 China GeoIP → 直连
//  4. 其它 → 走代理
func shouldBypass(host string) bool {
	// 1) LAN / 本机
	if isLanHost(host) {
		return true
	}

	// 2) 明显的国内域名后缀（避免一次 DNS+mmdb 查询，常见域名秒判）
	if isCNDomainSuffix(host) {
		return true
	}

	// 3) IP 字面量直接查 GeoIP
	if ip := net.ParseIP(host); ip != nil {
		return isChinaIP(ip)
	}

	// 4) 域名 → 解析 IP → 查 GeoIP
	resolver := net.Resolver{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		// 解析失败：保守走代理（避免未知流量被误放行）
		return false
	}
	for _, ip := range ips {
		if isChinaIP(ip.IP) {
			return true
		}
	}
	return false
}

// isCNDomainSuffix 快速判定明显中国的域名后缀
// 命中即直连，省一次 DNS+GeoIP 查询
func isCNDomainSuffix(host string) bool {
	host = strings.ToLower(host)
	// 以 .cn 结尾（含 .com.cn / .net.cn / .org.cn 等）
	if strings.HasSuffix(host, ".cn") {
		return true
	}
	// 常见国内二级域名（可按需扩展）
	cnDomains := []string{
		".com.cn", ".net.cn", ".org.cn", ".gov.cn", ".edu.cn",
		".ac.cn", ".sh.cn", ".bj.cn", ".tianjin.cn", ".gd.cn",
	}
	for _, d := range cnDomains {
		if strings.HasSuffix(host, d) {
			return true
		}
	}
	return false
}

// isLanHost 判断主机是否属于局域网/本机（应直连）
// 规则:
//   - IP 字面量: loopback / 私有 / 链路本地 / 未指定地址
//   - 域名: ".local"、无点主机名(如 "myserver") 视为本机；
//     其余域名做一次短超时 DNS 解析再按 IP 判断
func isLanHost(host string) bool {
	// 1) IP 字面量直接判断
	if ip := net.ParseIP(host); ip != nil {
		return isLanIP(ip)
	}

	// 2) 明显的本地域名后缀
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	// 无点的主机名通常是局域网内主机名
	if !containsDot(host) {
		return true
	}

	// 3) 域名做一次短超时 DNS 解析，任一 IP 命中即视为局域网
	resolver := net.Resolver{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		// 解析失败：保守起见不直连（避免把未知流量绕过代理）
		return false
	}
	for _, ip := range ips {
		if isLanIP(ip.IP) {
			return true
		}
	}
	return false
}

// isLanIP 判断 IP 是否本机/局域网
func isLanIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// directRelay 不经过 SS 加密，直接在客户端与目标之间双向中转（SOCKS5 场景）
// 调用方负责在返回后关闭 client。dst 为 "host:port"
func directRelay(client net.Conn, dst string) {
	directRelayHTTP(client, dst, true, nil)
}

// directRelayHTTP 不经过 SS 加密的直连中转（socks 与 http 通用）
//
//	isConnect = true  : 表示握手已完成（SOCKS5 已回 success，或 HTTP 拨号之前），
//	                     直接双向透明转发
//	isConnect = false : 普通 HTTP 代理请求，需把 rest（原始请求字节，已剥 hop 头）
//	                     先写到目标，再双向透明转发
func directRelayHTTP(client net.Conn, dst string, isConnect bool, rest []byte) {
	remote, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		log.Printf("[直连] 连接 %s 失败: %v", dst, err)
		client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer remote.Close()

	// 普通 HTTP：对 CONNECT 回 200；对普通请求必须先发首包
	if isConnect {
		client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	} else if len(rest) > 0 {
		// 把原始请求（含可能的首段 body）投递给真实后端
		if _, err := remote.Write(rest); err != nil {
			log.Printf("[直连] 写入首包到 %s 失败: %v", dst, err)
			return
		}
	}

	relay(client, remote)
}

// --- 小工具 ---

// relay 双向透明转发：在 conn1/conn2 之间互拷，直到任意一侧断开
// 调用方负责 conn1/conn2 的生命周期与关闭
func relay(conn1, conn2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(conn2, conn1) }()
	go func() { defer wg.Done(); io.Copy(conn1, conn2) }()
	wg.Wait()
}

func containsDot(s string) bool {
	return strings.Contains(s, ".")
}
