package main

import "errors"

var (
	errInvalidIdx = errors.New("无效的节点索引")
	errNoServers  = errors.New("请先添加服务器")
)
