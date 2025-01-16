package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func findAPMDir(path string) (string, error) {
	path = filepath.Clean(path)
	for {
		if filepath.Base(path) == "apm" {
			return path, nil
		}
		nextPath := filepath.Dir(path)
		if nextPath == path { // 到达根目录
			return "", fmt.Errorf("no 'apm' directory found in path: %s", path)
		}
		path = nextPath
	}
}

// detectAPMInstallPath lists all the dynamic libraries loaded by a process
func detectAPMInstallPath(pid int) (string, error) {
	mapsFile := fmt.Sprintf("/proc/%d/maps", pid)
	file, err := os.Open(mapsFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) > 5 {
			path := fields[len(fields)-1]

			if !strings.HasPrefix(path, "/") {
				continue
			}

			// exp: /opt/bonree/apm/agent/go/4.11.0-alpha1/lib/libagentgo-linux-x86_64.so
			if strings.HasSuffix(path, "libagentgo-linux-x86_64.so") {
				dir, _ := findAPMDir(path)
				if dir != "" {
					return dir, nil
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read %s: %w", mapsFile, err)
	}

	return "", nil
}
