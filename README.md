# profiling-go
profiling-go 是一个基于 dd-trace-go 的封装，用于采集 Go 应用的持续剖析数据，并将其上报到Bonree One平台中。

## 使用说明

### 1. 添加依赖

修改项目的 go.mod 文件

```
require github.com/bonree-smartagent/profiling-go latest
```

然后运行：

```bash
go mod tidy
```

### 2. 修改代码

```go
package main

import (
	"github.com/bonree-smartagent/profiling-go/profiler"
)

func main() {
	profiler.Start()
	defer profiler.Stop()
    // 业务代码
}
```

### 3. 编译

```bash
# 确保添加环境变量 CGO_ENABLED=1
# 如果go编译环境为：musl，则还需添加编译参数 -ldflags="-linkmode=external"
CGO_ENABLED=1 go build -ldflags="-linkmode=external" main.go
```

4. 安装smartagent，配置监控应用