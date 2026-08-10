//go:build !web

package runtimepath

// RootDir 返回桌面应用的运行根目录。
//
// 桌面发布包可以从任意工作目录启动，因此优先使用可执行文件所在目录；仅在无法
// 解析可执行文件时回退到当前工作目录。
func RootDir() string {
	if dir, ok := executableDirectory(); ok {
		return dir
	}
	if wd, ok := workingDirectory(); ok {
		return wd
	}
	return "."
}
