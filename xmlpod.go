package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/cespare/xxhash"
	plist "howett.net/plist"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	SessionCreate            bool                   `plist:"SessionCreate"`
}

func getServiceLabelForPod(pod *corev1.Pod) string {
	return fmt.Sprintf("macos-virtual-kubelet.pod.%d", xxhash.Sum64String(fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)))
}

func getServiceLabelForNamespaceAndName(namespace string, name string) string {
	return fmt.Sprintf("macos-virtual-kubelet.pod.%d", xxhash.Sum64String(fmt.Sprintf("%s/%s", namespace, name)))
}

func xmlToPod(path string) (*corev1.Pod, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	bs := make([]byte, stat.Size())
	_, err = bufio.NewReader(file).Read(bs)
	if err != nil && err != io.EOF {
		return nil, err
	}

	var agent plistAgent
	_, err = plist.Unmarshal(bs, &agent)
	if err != nil {
		return nil, err
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
	}, nil
}

func podToXml(pod *corev1.Pod) (string, []byte, error) {
	if !pod.Spec.HostNetwork {
		return "", nil, fmt.Errorf("pod must have 'hostNetwork' set to true")
	}
	if pod.Spec.AutomountServiceAccountToken == nil ||
		*pod.Spec.AutomountServiceAccountToken {
		return "", nil, fmt.Errorf("pod must have 'automountServiceAccountToken' set to false")
	}
	if len(pod.Spec.Containers) != 1 {
		return "", nil, fmt.Errorf("pod must have exactly one container")
	}
	if len(pod.Spec.EphemeralContainers) != 0 {
		return "", nil, fmt.Errorf("pod must have no ephemeral containers")
	}
	if len(pod.Spec.InitContainers) != 0 {
		return "", nil, fmt.Errorf("pod must have no init containers")
	}
	if len(pod.Spec.Volumes) != 0 {
		return "", nil, fmt.Errorf("pod must have no volumes")
	}
	if len(pod.Spec.Containers[0].EnvFrom) != 0 {
		return "", nil, fmt.Errorf("pod container must have no 'envFrom' entries")
	}

	envVars := make(map[string]string)
	for _, envVar := range pod.Spec.Containers[0].Env {
		if envVar.ValueFrom != nil {
			return "", nil, fmt.Errorf("pod container env var %s must not use 'valueFrom'", envVar.Name)
		}
		envVars[envVar.Name] = envVar.Value
	}

	label := getServiceLabelForPod(pod)

	var args []string
	args = append(args, pod.Spec.Containers[0].Command...)
	args = append(args, pod.Spec.Containers[0].Args...)
	if len(args) == 0 {
		return "", nil, fmt.Errorf("pod container must set 'command' or 'args'; macOS images do not embed execution information")
	}

	var terminationPeriodSeconds int64
	terminationPeriodSeconds = 60
	if pod.Spec.TerminationGracePeriodSeconds != nil {
		terminationPeriodSeconds = *pod.Spec.TerminationGracePeriodSeconds
	}

	agent := plistAgent{
		EnvironmentVariables:     envVars,
		KeepAlive:                false,
		Label:                    label,
		ProgramArguments:         args,
		RunAtLoad:                false,
		StandardErrorPath:        path.Join(os.TempDir(), fmt.Sprintf("log.%s", label)),
		StandardOutPath:          path.Join(os.TempDir(), fmt.Sprintf("log.%s", label)),
		ThrottleInterval:         10,
		Debug:                    false,
		KubernetesName:           pod.Name,
		KubernetesNamespace:      pod.Namespace,
		KubernetesUID:            string(pod.UID),
		KubernetesLabels:         pod.Labels,
		KubernetesAnnotations:    pod.Annotations,
		KubernetesContainerName:  pod.Spec.Containers[0].Name,
		KubernetesContainerImage: pod.Spec.Containers[0].Image,
		WorkingDirectory:         os.TempDir(),
		ExitTimeOut:              int(terminationPeriodSeconds),
		// @note: These settings allow Kubernetes pods to interact with the desktop and Keychain, which is required for some macOS workloads.
		ProcessType:   "Interactive",
		SessionCreate: false,
	}

	bs, err := plist.Marshal(agent, plist.XMLFormat)
	if err != nil {
		return "", nil, err
	}

	return label, bs, nil
}
