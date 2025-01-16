package internal

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type AgentType int32

type Tech struct {
	Value   string `json:"value,omitempty"`
	Version string `json:"version,omitempty"`
}

type ProcessData struct {
	PID               int32    `json:"pid,omitempty"`
	PPID              int32    `json:"ppid,omitempty"`
	ProcessGroupName  string   `json:"processGroupName,omitempty"`
	ProcessGroupIndex int      `json:"processGroupIndex,omitempty"`
	DetectionType     string   `json:"detectionType,omitempty"`
	Type              int      `json:"type,omitempty"`
	Techs             []Tech   `json:"techs,omitempty"`
	State             string   `json:"state,omitempty"`
	StartTime         int64    `json:"startTime,omitempty"`
	Name              string   `json:"name,omitempty"`
	Cmd               string   `json:"cmd,omitempty"`
	ContainerId       string   `json:"containerId,omitempty"`
	ListenPorts       []string `json:"listenPorts,omitempty"`
	ProcessUID        string   `json:"processUID,omitempty"`
	AgentUID          string   `json:"agentUID,omitempty"`
	AgentVersion      string   `json:"agentVersion,omitempty"`
	Cpu               float64  `json:"cpu,omitempty"`
}

type ResponseData struct {
	Code int `json:"code,omitempty"`
	Data struct {
		QueryTime         string        `json:"queryTime,omitempty"`
		UniqueHost        string        `json:"uniqueHost,omitempty"`
		EnableHostMonitor string        `json:"enableHostMonitor,omitempty"`
		AccountGUID       string        `json:"accountGUID,omitempty"`
		EnvID             string        `json:"envId,omitempty"`
		Cluster           string        `json:"cluster,omitempty"`
		NetworkZone       string        `json:"networkzone,omitempty"`
		ContainerCount    string        `json:"containerCount,omitempty"`
		User              string        `json:"user,omitempty"`
		Group             string        `json:"group,omitempty"`
		Mode              string        `json:"mode,omitempty"`
		InjectMode        string        `json:"injectMode,omitempty"`
		Processes         []ProcessData `json:"processes,omitempty"`
	} `json:"data,omitempty"`
}

// UnmarshalJSON for ProcessData to handle string to int conversion
func (p *ProcessData) UnmarshalJSON(data []byte) error {
	type Alias ProcessData
	aux := &struct {
		PID               string `json:"pid,omitempty"`
		Cpu               string `json:"cpu,omitempty"`
		PPID              string `json:"ppid,omitempty"`
		ProcessGroupIndex string `json:"processGroupIndex,omitempty"`
		Type              string `json:"type,omitempty"`
		StartTime         string `json:"startTime,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var v, _ = strconv.Atoi(aux.PID)
	p.PID = int32(v)
	v, _ = strconv.Atoi(aux.PPID)
	p.PPID = int32(v)
	p.Cpu, _ = strconv.ParseFloat(aux.Cpu, 64)
	p.ProcessGroupIndex, _ = strconv.Atoi(aux.ProcessGroupIndex)
	p.Type, _ = strconv.Atoi(aux.Type)
	p.StartTime, _ = strconv.ParseInt(aux.StartTime, 10, 64)

	return nil
}

// UnmarshalJSON for ResponseData to handle string to int conversion
func (r *ResponseData) UnmarshalJSON(data []byte) error {
	type Alias ResponseData
	aux := &struct {
		Code string `json:"code,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	r.Code, _ = strconv.Atoi(aux.Code)

	return nil
}

func getProcessList(serverIP string, key string) (*ResponseData, error) {
	var url = fmt.Sprintf("http://%s/v1/processes", serverIP)
	reqMap := map[string]string{}
	reqMap["searchKey"] = key
	requestBody, err := json.Marshal(reqMap)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var responseData ResponseData
	err = json.Unmarshal(body, &responseData)
	if err != nil {
		return nil, err
	}

	return &responseData, nil
}

func detectAgentUID() (string, error) {
	serverIP, err := getMachineServerIP()
	if err != nil {
		return "", err
	}

	pid := os.Getpid()
	searchKey := strconv.Itoa(pid)
	if containerId != "" {
		searchKey = containerId
	}

	responseData, err := getProcessList(serverIP, searchKey)
	if err != nil {
		return "", err
	}

	if responseData.Code != 0 || len(responseData.Data.Processes) == 0 {
		return "", nil
	}

	// 获取当前进程命令行
	cmdline := strings.Join(os.Args, " ")

	for _, process := range responseData.Data.Processes {
		if process.AgentUID == "" {
			continue
		}

		if containerId == "" && pid != int(process.PID) {
			continue
		}

		if containerId != process.ContainerId {
			continue
		}

		if cmdline != process.Cmd {
			continue
		}

		if process.State != "AGENT_STATE_APP_RESTART_TO_UPDATE_AGENT" && process.State != "AGENT_STATE_APP_MONITORED" {
			_ = log.Warnf("Agent state is %s", process.State)
			continue
		}

		log.Infof("Found Agent UID: %s", process.AgentUID)
		return process.AgentUID, nil
	}

	return "", nil
}

type HeaderRoundTripper struct {
	Transport http.RoundTripper
	Headers   http.Header
}

func (h *HeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	reqClone := req.Clone(req.Context())
	reqClone.Header.Del("Dd-Api-Key")
	reqClone.Header.Set("x-br-acid", getAccountGUID())
	reqClone.Header.Set("x-br-aid", getAgentUID())

	if h.Transport == nil {
		h.Transport = http.DefaultTransport
	}
	return h.Transport.RoundTrip(reqClone)
}

func NewClient() *http.Client {
	return &http.Client{
		Transport: &HeaderRoundTripper{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}
