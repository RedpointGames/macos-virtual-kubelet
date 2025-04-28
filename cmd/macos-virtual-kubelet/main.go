package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	plist "howett.net/plist"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	buildVersion = "local"
	buildTime    = "N/A"
	k8sVersion   = "v1.26.8"
)

type MacOSProvider struct {
	nodeutil.Provider
}

const (
	DefaultPods = 110
)

var (
	errNotImplemented = fmt.Errorf("not implemented by macOS provider")
)

type plistSocket struct {
	SockServiceName string `plist:"Label"`
	SockType        string `plist:"Label"`
	SockFamily      string `plist:"Label"`
}

// Supported resource constraints:
// CPU: CPU time in seconds
// ResidentSetSize: Memory in bytes
// NumberOfFiles: Maximum number of open files
// NumberOfProcesses: Maximum number of simultatneous processes for this user ID?

// https://manpagez.com/man/5/launchd.plist/
type plistAgent struct {
	EnvironmentVariables     map[string]string      `plist:"EnvironmentVariables"`
	KeepAlive                bool                   `plist:"KeepAlive"`
	Label                    string                 `plist:"Label"`
	ProgramArguments         []string               `plist:"ProgramArguments"`
	RunAtLoad                bool                   `plist:"RunAtLoad"`
	StandardErrorPath        string                 `plist:"StandardErrorPath"`
	StandardOutPath          string                 `plist:"StandardOutPath"`
	ThrottleInterval         int                    `plist:"ThrottleInterval"`
	Debug                    bool                   `plist:"Debug"`
	SoftResourceLimits       map[string]int         `plist:"SoftResourceLimits"`
	HardResourceLimits       map[string]int         `plist:"HardResourceLimits"`
	Listeners                map[string]plistSocket `plist:"Sockets"`
	KubernetesName           string                 `plist:"KubernetesName"`
	KubernetesNamespace      string                 `plist:"KubernetesNamespace"`
	KubernetesUID            string                 `plist:"KubernetesUID"`
	KubernetesLabels         map[string]string      `plist:"KubernetesLabels"`
	KubernetesAnnotations    map[string]string      `plist:"KubernetesAnnotations"`
	KubernetesContainerName  string                 `plist:"KubernetesContainerName"`
	KubernetesContainerImage string                 `plist:"KubernetesContainerImage"`
	WorkingDirectory         string                 `plist:"WorkingDirectory"`
	ExitTimeOut              int                    `plist:"ExitTimeOut"`
	ProcessType              string                 `plist:"ProcessType"`
}

func xmlToPod(path string) *corev1.Pod {
	file, err := os.Open(path)
	if err != nil {
		log.L.Fatal(err)
		return nil
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		log.L.Fatal(err)
		return nil
	}
	bs := make([]byte, stat.Size())
	_, err = bufio.NewReader(file).Read(bs)
	if err != nil && err != io.EOF {
		log.L.Fatal(err)
		return nil
	}

	var agent plistAgent
	_, err = plist.Unmarshal(bs, &agent)
	if err != nil {
		log.L.Fatal(err)
		return nil
	}

	return &corev1.Pod{
		TypeMeta: v1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:        agent.KubernetesName,
			Namespace:   agent.KubernetesNamespace,
			UID:         types.UID(agent.KubernetesUID),
			Labels:      agent.KubernetesLabels,
			Annotations: agent.KubernetesAnnotations,
		},
		Spec: corev1.PodSpec{
			Volumes:        []corev1.Volume{},
			InitContainers: []corev1.Container{},
			Containers: []corev1.Container{
				// @todo: rest of this translation
				{
					Name:                     agent.KubernetesContainerName,
					Image:                    agent.KubernetesContainerImage,
					Command:                  agent.ProgramArguments,
					Args:                     []string{},
					WorkingDir:               agent.WorkingDirectory,
					Ports:                    []corev1.ContainerPort{},
					EnvFrom:                  []corev1.EnvFromSource{},
					Env:                      []corev1.EnvVar{},
					Resources:                corev1.ResourceRequirements{},
					ResizePolicy:             []corev1.ContainerResizePolicy{},
					VolumeMounts:             []corev1.VolumeMount{},
					VolumeDevices:            []corev1.VolumeDevice{},
					LivenessProbe:            &corev1.Probe{},
					ReadinessProbe:           &corev1.Probe{},
					StartupProbe:             &corev1.Probe{},
					Lifecycle:                &corev1.Lifecycle{},
					TerminationMessagePath:   path,
					TerminationMessagePolicy: "",
					ImagePullPolicy:          "",
					SecurityContext:          &corev1.SecurityContext{},
					Stdin:                    false,
					StdinOnce:                false,
					TTY:                      false,
				},
			},
			EphemeralContainers:           []corev1.EphemeralContainer{},
			RestartPolicy:                 "",
			TerminationGracePeriodSeconds: new(int64),
			ActiveDeadlineSeconds:         new(int64),
			DNSPolicy:                     "",
			NodeSelector:                  map[string]string{},
			ServiceAccountName:            "",
			DeprecatedServiceAccount:      "",
			AutomountServiceAccountToken:  new(bool),
			NodeName:                      "",
			HostNetwork:                   false,
			HostPID:                       false,
			HostIPC:                       false,
			ShareProcessNamespace:         new(bool),
			SecurityContext:               &corev1.PodSecurityContext{},
			ImagePullSecrets:              []corev1.LocalObjectReference{},
			Hostname:                      "",
			Subdomain:                     "",
			Affinity:                      &corev1.Affinity{},
			SchedulerName:                 "",
			Tolerations:                   []corev1.Toleration{},
			HostAliases:                   []corev1.HostAlias{},
			PriorityClassName:             "",
			Priority:                      new(int32),
			DNSConfig:                     &corev1.PodDNSConfig{},
			ReadinessGates:                []corev1.PodReadinessGate{},
			RuntimeClassName:              new(string),
			EnableServiceLinks:            new(bool),
			PreemptionPolicy:              nil,
			Overhead:                      corev1.ResourceList{},
			TopologySpreadConstraints:     []corev1.TopologySpreadConstraint{},
			SetHostnameAsFQDN:             new(bool),
			OS:                            &corev1.PodOS{},
			HostUsers:                     new(bool),
			SchedulingGates:               []corev1.PodSchedulingGate{},
			ResourceClaims:                []corev1.PodResourceClaim{},
		},
		Status: corev1.PodStatus{
			Phase:                      "",
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
		},
	}
}

func NewMacOSProvider() *MacOSProvider {
	return &MacOSProvider{}
}

func (p *MacOSProvider) CreatePod(ctx context.Context, pod *corev1.Pod) error {
	return errNotImplemented
}

func (p *MacOSProvider) UpdatePod(ctx context.Context, pod *corev1.Pod) error {
	return errNotImplemented
}

func (p *MacOSProvider) DeletePod(ctx context.Context, pod *corev1.Pod) error {
	return errNotImplemented
}

func (p *MacOSProvider) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	return nil, errNotImplemented
}

func (p *MacOSProvider) GetPodStatus(ctx context.Context, namespace, name string) (*corev1.PodStatus, error) {
	return nil, errNotImplemented
}

func (p *MacOSProvider) GetPods(ctx context.Context) ([]*corev1.Pod, error) {
	home := os.Getenv("HOME")
	folder := home + "/Library/LaunchAgents"
	files, err := ioutil.ReadDir(folder)
	if err != nil {
		log.G(ctx).Fatal(err)
		return nil, err
	}

	var pods []*corev1.Pod
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".plist") && strings.HasPrefix(f.Name(), "macos-virtual-kubelet.pod.") {
			pod := xmlToPod(path.Join(folder, f.Name()))
			if pod != nil {
				pods = append(pods, pod)
			}
		}
	}

	return pods, nil
}

func (p *MacOSProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	log.G(ctx).Infof("Received GetContainerLogs request for %s/%s/%s.\n", namespace, podName, containerName)
	return nil, errNotImplemented
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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	newProvider := func(cfg nodeutil.ProviderConfig) (nodeutil.Provider, node.NodeProvider, error) {
		return &MacOSProvider{}, nil, nil
	}

	cm, err := nodeutil.NewNode("macos", newProvider)
	if err != nil {
		log.G(ctx).Fatal(err)
	}
	go cm.Run(ctx)

	defer func() {
		log.G(ctx).Debug("Waiting for controllers to be done")
		cancel()
		<-cm.Done()
	}()

	duration, _ := time.ParseDuration("60s")

	log.G(ctx).Info("Waiting for controller to be ready")
	if err := cm.WaitReady(ctx, duration); err != nil {
		log.G(ctx).Fatal(err)
	}

	log.G(ctx).Info("Ready")

	select {
	case <-ctx.Done():
	case <-cm.Done():
		log.G(ctx).Fatal(err)
	}
}
