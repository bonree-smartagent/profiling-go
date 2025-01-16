package internal

import (
	"bytes"
	"fmt"
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
	"os"
	"strings"
	"sync"
	"time"
)

const machineServiceInfoPath = "/var/lib/bonree/apm/conf/machine_server.ip"
const containerInfoPath = "/var/lib/bonree/apm/conf/container.conf"

const agentUidEnvKey = "BR_AGENT_UID"
const deploymentMetadataKey = "BR_DEPLOYMENT_METADATA"
const controllerUrlEnvKey = "BR_APM_CONTROLLER_URL"
const accountGuidEnvKey = "BR_ACCOUNT_GUID"

var (
	apmDir      string
	containerId string

	configPath string
	config     *Config

	agentUID  string
	isFromEnv bool
	mu        sync.RWMutex
)

type Config struct {
	Common struct {
		ControllerURL string `yaml:"controllerUrl"`
		AccountGUID   string `yaml:"accountGUID"`
	} `yaml:"common"`
}

func getAgentUIDFromEnv() string {
	return os.Getenv(agentUidEnvKey)
}

func Init() (bool, error) {
	// just for internal test
	agentUID = getAgentUIDFromEnv()
	if agentUID != "" {
		isFromEnv = true
		containerId, _ = getContainerId()
		InitConfig()
		return true, nil
	} else {
		isFromEnv = false
	}

	if isServerless() {
		return false, fmt.Errorf("agent is not supported in serverless mode")
	}

	if !isApmInjected() {
		return false, fmt.Errorf("agent is not injected")
	}

	configPath = apmDir + "/conf/common.yml"

	containerId, _ = getContainerId()

	InitConfig()

	return true, nil
}

// InitConfig 初始化配置，带文件监控功能
func InitConfig() {
	loadConfig()
	go watchConfigFile()
}

// loadConfig 加载配置文件
func loadConfig() {
	newConfig := getConfig()

	mu.Lock()
	config = newConfig
	mu.Unlock()
	log.Info("Config reloaded successfully.")
}

// watchConfigFile 监控配置文件的实时变化
func watchConfigFile() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Errorf("Failed to create file watcher: %v", err)
	}
	defer watcher.Close()

	err = watcher.Add(configPath)
	if err != nil {
		_ = log.Errorf("Failed to watch config file: %v", err)
	}

	log.Debugf("Started watching config file: %s", configPath)

	for {
		select {
		case event := <-watcher.Events:
			if len(event.Name) == 0 {
				break
			}
			if event.Op^fsnotify.Chmod == 0 {
				break
			}

			// 检测到文件写入或更改事件
			log.Info("Config file changed, reloading...")
			loadConfig()
			watcher.Add(configPath)
		case err := <-watcher.Errors:
			log.Errorf("Watcher error: %v", err)
		}
	}
}

func getAgentUID() string {
	mu.RLock()
	defer mu.RUnlock()
	return agentUID
}

func isApmInjected() bool {
	dir, err := detectAPMInstallPath(os.Getpid())
	if err != nil {
		_ = log.Warnf("Error finding apm directory: %v", err)
		return false
	}

	apmDir = dir
	return true
}

func isServerless() bool {
	return os.Getenv(deploymentMetadataKey) == "mode=serverless"
}

func getConfig() *Config {
	url := os.Getenv(controllerUrlEnvKey)
	accountGUID := os.Getenv(accountGuidEnvKey)

	if url != "" && accountGUID != "" {
		var c Config
		c.Common.ControllerURL = url
		c.Common.AccountGUID = accountGUID
		return &c
	}

	file, err := os.Open(configPath)
	if err != nil {
		_ = log.Warnf("Error opening file: %v\n", err)
		return nil
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	var c Config
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&c); err != nil {
		_ = log.Warnf("Error decoding common.yml YAML: %v\n", err)
		return nil
	}

	log.Infof("Controller URL: %s", c.Common.ControllerURL)
	log.Infof("Account GUID: %s", c.Common.AccountGUID)
	return &c
}

func getMachineServerIP() (string, error) {
	content, err := os.ReadFile(machineServiceInfoPath)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(content)), nil
}

// RunLoop 循环检测配置文件变化，并按需启停采集
// @param start 启动采集回调，返回值为是否继续检查
// @param stop 停止采集回调
func RunLoop(start func() bool, stop func()) {
	isStarted := false
	for {
		if isFromEnv {
			time.Sleep(1 * time.Second)
			if isStarted {
				return
			}
		} else if isStarted {
			time.Sleep(5 * time.Minute)
		} else {
			time.Sleep(30 * time.Second)
		}
		if getAccountGUID() == "" || GetUrl() == "" {
			stop()
			continue
		}

		if !isFromEnv {
			agentuid, err := detectAgentUID()
			if err != nil {
				_ = log.Warnf("Failed to getting agent UID: %v", err)
				stop()
				continue
			}

			if agentuid == "" {
				stop()
				continue
			}
			log.Infof("Got Agent UID: %s", agentuid)
			agentUID = agentuid
		}

		isStarted = true
		if !start() {
			return
		}
	}
}

func getContainerId() (string, error) {
	buf, err := os.ReadFile(containerInfoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		if os.IsPermission(err) {
			return "", fmt.Errorf("permission denied: %w", err)
		}
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	content := string(buf)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "container.id") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return "", fmt.Errorf("invalid format in container info file: %s", line)
			}
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", fmt.Errorf("container.id not found in the file")
}
func GetUrl() string {
	mu.RLock()
	defer mu.RUnlock()
	if config == nil {
		return ""
	}
	return config.Common.ControllerURL + "/profiling"
}

func getAccountGUID() string {
	mu.RLock()
	defer mu.RUnlock()
	if config == nil {
		return ""
	}
	return config.Common.AccountGUID
}
