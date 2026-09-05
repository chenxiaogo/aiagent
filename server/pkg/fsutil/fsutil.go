// Package fsutil 提供平台共享的文件系统根目录解析，且不依赖任何业务包，
// 以避免 handler / service 之间的 import 循环。
package fsutil

import "os"

// ExportsDir 返回平台 HTML 产物（导出/分享）落盘根目录。
// write_local_file 与 chat handler 的 ExportMessage/ExportStatic 共用此根，
// 保证「写入在哪、从哪读」同源（默认 data/exports，受 AIA_EXPORTS_DIR 覆盖）。
func ExportsDir() string {
	if d := os.Getenv("AIA_EXPORTS_DIR"); d != "" {
		return d
	}
	return "data/exports"
}
