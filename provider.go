package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	plist "howett.net/plist"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MacOSProvider struct {
	nodeutil.Provider
}

var (
	errNotImplemented = fmt.Errorf("not implemented by macOS provider")
	launchAgentsPath  = path.Join(os.Getenv("HOME"), "Library", "LaunchAgents")
)

func NewMacOSProvider() *MacOSProvider {
	return &MacOSProvider{}
}

func (p *MacOSProvider) CreatePod(ctx context.Context, pod *corev1.Pod) error {
	log.G(ctx).Infof("Received CreatePod request for %s/%s.\n", pod.Namespace, pod.Name)

	label, xml, err := podToXml(pod)
	if err != nil {
		log.G(ctx).Errorf("Failed to convert pod to XML for %s/%s: %s", pod.Namespace, pod.Name, err.Error())
		return err
	}

	plistPath := path.Join(launchAgentsPath, fmt.Sprintf("%s.plist", label))
	file, err := os.Create(plistPath)
	if err != nil {
		log.G(ctx).Errorf("Failed to open file: %s", plistPath)
		return err
	}
	defer file.Close()

	_, err = bufio.NewWriter(file).Write(xml)
	if err != nil && err != io.EOF {
		log.G(ctx).Errorf("Failed to write to file: %s", plistPath)
		return err
	}

	launchCmd := exec.Command("launchctl", "load", plistPath)
	output, err := launchCmd.Output()
	log.G(ctx).Infof("'launchctl load' output: %s", output)
	if err != nil {
		os.Remove(plistPath)
		log.G(ctx).Errorf("Failed to load service: %s", plistPath)
		return err
	}

	launchCmd = exec.Command("launchctl", "start", label)
	output, err = launchCmd.Output()
	log.G(ctx).Infof("'launchctl start' output: %s", output)
	if err != nil {
		os.Remove(plistPath)
		log.G(ctx).Errorf("Failed to load service: %s", plistPath)
		return err
	}

	log.G(ctx).Infof("Successfully handled CreatePod request for %s/%s.\n", pod.Namespace, pod.Name)
	return nil
}

func (p *MacOSProvider) UpdatePod(ctx context.Context, pod *corev1.Pod) error {
	log.G(ctx).Infof("Received UpdatePod request for %s/%s.\n", pod.Namespace, pod.Name)

	label, xml, err := podToXml(pod)
	if err != nil {
		log.G(ctx).Errorf("Failed to convert pod to XML for %s/%s: %s", pod.Namespace, pod.Name, err.Error())
		return err
	}

	plistPath := path.Join(launchAgentsPath, fmt.Sprintf("%s.plist", label))
	file, err := os.Create(plistPath)
	if err != nil {
		log.G(ctx).Errorf("Failed to open file: %s", plistPath)
		return err
	}
	defer file.Close()

	_, err = bufio.NewWriter(file).Write(xml)
	if err != nil && err != io.EOF {
		log.G(ctx).Errorf("Failed to write to file: %s", plistPath)
		return err
	}

	log.G(ctx).Infof("Successfully handled UpdatePod request for %s/%s.\n", pod.Namespace, pod.Name)
	return nil
}

func (p *MacOSProvider) DeletePod(ctx context.Context, pod *corev1.Pod) error {
	log.G(ctx).Infof("Received DeletePod request for %s/%s.\n", pod.Namespace, pod.Name)

	label := getServiceLabelForPod(pod)
	plistPath := path.Join(launchAgentsPath, fmt.Sprintf("%s.plist", label))

	launchCmd := exec.Command("launchctl", "stop", label)
	output, _ := launchCmd.Output()
	log.G(ctx).Infof("'launchctl stop' output: %s", output)

	launchCmd = exec.Command("launchctl", "unload", plistPath)
	output, _ = launchCmd.Output()
	log.G(ctx).Infof("'launchctl unload' output: %s", output)

	err := os.Remove(plistPath)
	if err != nil {
		log.G(ctx).Errorf("Failed to delete file: %s", plistPath)
		return err
	}

	return nil
}

func (p *MacOSProvider) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	pod, err := xmlToPod(path.Join(launchAgentsPath, fmt.Sprintf("%s.plist", getServiceLabelForNamespaceAndName(namespace, name))))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		log.G(ctx).Errorf("Failed to read pod: %s", err.Error())
		return nil, nil
	}

	podStatus, err := p.GetPodStatus(ctx, namespace, name)
	if err != nil && podStatus != nil {
		pod.Status = *podStatus
	}

	return pod, err
}

type plistAgentStatus struct {
	StandardOutPath        string   `plist:"StandardOutPath"`
	LimitLoadToSessionType string   `plist:"LimitLoadToSessionType"`
	StandardErrorPath      string   `plist:"StandardErrorPath"`
	Label                  string   `plist:"Label"`
	OnDemand               bool     `plist:"OnDemand"`
	LastExitStatus         int      `plist:"LastExitStatus"`
	PID                    *int     `plist:"PID"`
	Program                string   `plist:"Program"`
	ProgramArguments       []string `plist:"ProgramArguments"`
}

func (p *MacOSProvider) GetPodStatus(ctx context.Context, namespace, name string) (*corev1.PodStatus, error) {
	launchCmd := exec.Command("launchctl", "list", getServiceLabelForNamespaceAndName(namespace, name))
	output, err := launchCmd.Output()
	if err != nil {
		log.G(ctx).Errorf("Failed to query pod status: %s", err)
		return nil, err
	}

	// We have to remove "ProgramArguments" because our plist library can't handle it
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	var linesStripped []string
	ignoring := false
	for _, line := range lines {
		if strings.Contains(line, "\"ProgramArguments\" =") {
			ignoring = true
		} else if ignoring && line == "};" {
			linesStripped = append(linesStripped, "}")
		}
		if !ignoring {
			linesStripped = append(linesStripped, line)
		}
	}

	statusplist := strings.Join(linesStripped[:], "\n")

	var agentStatus plistAgentStatus
	_, err = plist.Unmarshal([]byte(statusplist), &agentStatus)
	if err != nil {
		return nil, err
	}

	if agentStatus.PID == nil {
		// service is no longer running
		phase := "Succeeded"
		if agentStatus.LastExitStatus != 0 {
			phase = "Failed"
		}
		return &corev1.PodStatus{
			Phase:                      corev1.PodPhase(phase),
			Conditions:                 []corev1.PodCondition{},
			Message:                    fmt.Sprintf("process terminated with exit code %d", agentStatus.LastExitStatus),
			Reason:                     "",
			NominatedNodeName:          "",
			HostIP:                     "",
			PodIP:                      "",
			PodIPs:                     []corev1.PodIP{},
			StartTime:                  &v1.Time{},
			InitContainerStatuses:      []corev1.ContainerStatus{},
			ContainerStatuses:          []corev1.ContainerStatus{},
			QOSClass:                   "",
			EphemeralContainerStatuses: []corev1.ContainerStatus{},
			Resize:                     "",
		}, nil
	} else {
		// service is currently running
		return &corev1.PodStatus{
			Phase:                      "Running",
			Conditions:                 []corev1.PodCondition{},
			Message:                    "",
			Reason:                     "",
			NominatedNodeName:          "",
			HostIP:                     "",
			PodIP:                      "",
			PodIPs:                     []corev1.PodIP{},
			StartTime:                  &v1.Time{},
			InitContainerStatuses:      []corev1.ContainerStatus{},
			ContainerStatuses:          []corev1.ContainerStatus{},
			QOSClass:                   "",
			EphemeralContainerStatuses: []corev1.ContainerStatus{},
			Resize:                     "",
		}, nil
	}
}

func (p *MacOSProvider) GetPods(ctx context.Context) ([]*corev1.Pod, error) {
	files, err := ioutil.ReadDir(launchAgentsPath)
	if err != nil {
		log.G(ctx).Fatal(err)
		return nil, err
	}

	var pods []*corev1.Pod
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".plist") && strings.HasPrefix(f.Name(), "macos-virtual-kubelet.pod.") {
			pod, err := xmlToPod(path.Join(launchAgentsPath, f.Name()))
			if err != nil {
				return nil, err
			}
			if pod != nil {
				pods = append(pods, pod)
			}
		}
	}

	return pods, nil
}

func (p *MacOSProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	file, err := os.Open(path.Join(os.TempDir(), fmt.Sprintf("log.%s", getServiceLabelForNamespaceAndName(namespace, podName))))
	if err != nil {
		return nil, err
	}
	return file, err
}

func (p *MacOSProvider) RunInContainer(ctx context.Context, namespace, podName, containerName string, cmd []string, attach api.AttachIO) error {
	log.G(ctx).Infof("Received RunInContainer request for %s/%s/%s.\n", namespace, podName, containerName)
	return errNotImplemented
}

func (p *MacOSProvider) AttachToContainer(ctx context.Context, namespace, podName, containerName string, attach api.AttachIO) error {
	log.G(ctx).Infof("Received AttachToContainer request for %s/%s/%s.\n", namespace, podName, containerName)
	return errNotImplemented
}

func (p *MacOSProvider) GetStatsSummary(ctx context.Context) (*statsv1alpha1.Summary, error) {
	log.G(ctx).Info("Received GetStatsSummary request.\n")
	return nil, errNotImplemented
}

func (p *MacOSProvider) GetMetricsResource(ctx context.Context) ([]*dto.MetricFamily, error) {
	log.G(ctx).Info("Received GetMetricsResource request.\n")
	return nil, errNotImplemented
}

func (p *MacOSProvider) PortForward(ctx context.Context, namespace, pod string, port int32, stream io.ReadWriteCloser) error {
	log.G(ctx).Infof("Received PortForward request for %s/%s:%d.\n", namespace, pod, port)
	return errNotImplemented
}
