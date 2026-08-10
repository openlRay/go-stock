//go:build web

package runtimepath

// RootDir 返回 Web 进程的运行根目录。
//
// Web 开发通过 `go run -tags web .` 启动，其临时可执行文件不属于应用数据目录；
// Docker 则通过 WORKDIR /app 保证当前目录对应持久化卷根。因此 Web 模式优先使用
// 当前工作目录，仅在无法获取时回退到可执行文件目录。
func RootDir() string {
	if wd, ok := workingDirectory(); ok {
		return wd
	}
	if dir, ok := executableDirectory(); ok {
		return dir
	}
	return "."
}
