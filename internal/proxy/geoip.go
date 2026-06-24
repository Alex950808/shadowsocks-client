package proxy

import (
	_ "embed"
	"log"
	"net"
	"net/netip"
	"sync"

	"github.com/oschwald/geoip2-golang/v2"
)

//go:embed data/GeoLite2-Country.mmdb
var geoipMmdbBytes []byte

var (
	geoipReader *geoip2.Reader
	geoipOnce   sync.Once
	geoipOk     bool
)

// initGeoip 懒加载 mmdb（首次调用 isChinaIP 时触发）
// 失败不致命：分流降级为「只按 LAN 规则」，所有非 LAN 流量走代理
func initGeoip() {
	geoipOnce.Do(func() {
		r, err := geoip2.OpenBytes(geoipMmdbBytes)
		if err != nil {
			log.Printf("[GeoIP] 加载 mmdb 失败，国内分流降级为禁用: %v", err)
			return
		}
		geoipReader = r
		geoipOk = true
		log.Printf("[GeoIP] 已加载 GeoLite2-Country，国内分流启用")
	})
}

// isChinaIP 判断 IP 是否属于中国大陆
// IPv4/IPv6 均支持；mmdb 加载失败时返回 false（保守走代理）
func isChinaIP(ip net.IP) bool {
	initGeoip()
	if !geoipOk || geoipReader == nil {
		return false
	}
	// geoip2 v2 接 netip.Addr，转换一下
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	country, err := geoipReader.Country(addr)
	if err != nil {
		return false
	}
	return country.Country.ISOCode == "CN"
}
