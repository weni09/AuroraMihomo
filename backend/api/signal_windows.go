//go:build windows

package main

import "os"

// reloadSignal 在 Windows 上返回 nil：该平台没有 SIGHUP，
// 强行注册一个不存在的信号只会得到永不触发的通道。
// 这里如实返回 nil，热重载改由 POST /api/v1/system/reload 触发。
func reloadSignal() os.Signal { return nil }
