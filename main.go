package main

// Heavily adapted from https://github.com/agoda-com/macOS-vz-kubelet to get the basic scaffolding done.

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/virtual-kubelet/virtual-kubelet/log"
	logruslogger "github.com/virtual-kubelet/virtual-kubelet/log/logrus"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"

	sigar "github.com/cloudfoundry/gosigar"
	"github.com/denisbrodbeck/machineid"
	"github.com/mitchellh/go-homedir"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
)

var (
	k8sVersion = "v1.26.1" // This should follow the version of k8s.io we are importing

	logLevel        = "info"
	traceSampleRate string

	// k8s
	kubeConfigPath  = os.Getenv("KUBECONFIG")
	startupTimeout  time.Duration
	numberOfWorkers               = 10
	resync          time.Duration = 1 * time.Minute
	providerID      string

	certPath   string
	keyPath    string
	caCertPath string

	nodeName   = "vk-macos-vz-test"
	listenPort = 10250
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	binaryName := filepath.Base(os.Args[0])
	desc := binaryName + " implements a node on a Kubernetes cluster where pods run directly as processes on the host."

	if kubeConfigPath == "" {
		home, _ := homedir.Dir()
		if home != "" {
			kubeConfigPath = filepath.Join(home, ".kube", "config")
		}
	}
	k8sClient, err := nodeutil.ClientsetFromEnv(kubeConfigPath)
	if err != nil {
		log.G(ctx).Fatal(err)
	}

	cmd := &cobra.Command{
		Use:   binaryName,
		Short: desc,
		Long:  desc,
		Run: func(cmd *cobra.Command, args []string) {
			logger := logrus.StandardLogger()
			lvl, err := logrus.ParseLevel(logLevel)
			if err != nil {
				logrus.WithError(err).Fatal("Error parsing log level")
			}
			logger.SetLevel(lvl)

			log.L = logruslogger.FromLogrus(logrus.NewEntry(logger))

			// Set the default logger
			ctx := log.WithLogger(cmd.Context(), log.L)
			if err := run(ctx, k8sClient); err != nil {
				if !errors.Is(err, context.Canceled) {
					log.L.Fatal(err)
				}
				log.L.Debug(err)
			}
		},
	}
	flags := cmd.Flags()

	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	klogFlags.VisitAll(func(f *flag.Flag) {
		f.Name = "klog." + f.Name
		flags.AddGoFlag(f)
	})

	hostName, err := os.Hostname()
	if err != nil {
		log.G(ctx).Fatal(err)
	}
	// lowercase RFC 1123 subdomain
	hostName = strings.ToLower(hostName)

	flags.StringVar(&certPath, "node-cert-path", certPath, "[required] path to node certificate")
	flags.StringVar(&keyPath, "node-key-path", keyPath, "[required] path to node certificate")
	flags.StringVar(&caCertPath, "ca-cert-path", caCertPath, "[required] path to node certificate")

	flags.StringVar(&nodeName, "nodename", hostName, "kubernetes node name")
	flags.StringVar(&providerID, "provider-id", providerID, "provider ID to report to the Kubernetes API server")
	flags.DurationVar(&startupTimeout, "startup-timeout", startupTimeout, "How long to wait for the virtual-kubelet to start")
	flags.StringVar(&logLevel, "log-level", logLevel, "log level.")
	flags.IntVar(&numberOfWorkers, "pod-sync-workers", numberOfWorkers, `set the number of pod synchronization workers`)
	flags.DurationVar(&resync, "full-resync-period", resync, "how often to perform a full resync of pods between kubernetes and the provider")

	flags.StringVar(&traceSampleRate, "trace-sample-rate", traceSampleRate, "set probability of tracing samples")

	if err := cmd.ExecuteContext(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			logrus.WithError(err).Fatal("Error running command")
		}
	}
}

func withProviderID(cfg *nodeutil.NodeConfig) error {
	cfg.NodeSpec.Spec.ProviderID = providerID
	return nil
}

func withVersion(cfg *nodeutil.NodeConfig) error {
	cfg.NodeSpec.Status.NodeInfo.KubeletVersion = strings.Join([]string{k8sVersion, "vk-macos-host"}, "-")
	return nil
}

func configureRoutes(cfg *nodeutil.NodeConfig) error {
	mux := http.NewServeMux()
	cfg.Handler = mux
	return nodeutil.AttachProviderRoutes(mux)(cfg)
}

func withClient(c kubernetes.Interface, cfg *nodeutil.NodeConfig) error {
	return nodeutil.WithClient(c)(cfg)
}

func withCA(cfg *tls.Config) error {
	if caCertPath == "" {
		return nil
	}
	if err := nodeutil.WithCAFromPath(caCertPath)(cfg); err != nil {
		return fmt.Errorf("error getting CA from path: %w", err)
	}
	return nil
}

func run(ctx context.Context, c kubernetes.Interface) error {
	if caCertPath == "" {
		return fmt.Errorf("--ca-cert-path must be set")
	}
	if certPath == "" {
		return fmt.Errorf("--node-cert-path must be set")
	}
	if keyPath == "" {
		return fmt.Errorf("--node-key-path must be set")
	}

	node, err := nodeutil.NewNode(nodeName,
		func(cfg nodeutil.ProviderConfig) (nodeutil.Provider, node.NodeProvider, error) {
			if port := os.Getenv("KUBELET_PORT"); port != "" {
				kubeletPort, err := strconv.ParseInt(port, 10, 32)
				if err != nil {
					return nil, nil, err
				}
				listenPort = int(kubeletPort)
			}
			_, _, _, err := host.PlatformInformationWithContext(ctx)
			if err != nil {
				return nil, nil, err
			}

			eventBroadcaster := record.NewBroadcaster()
			eventBroadcaster.StartLogging(log.G(ctx).Infof)
			eventBroadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: c.CoreV1().Events(corev1.NamespaceAll)})

			p := NewMacOSProvider()
			return p, nil, nil
		},
		func(cfg *nodeutil.NodeConfig) error {
			return withClient(c, cfg)
		},
		func(cfg *nodeutil.NodeConfig) error {

			var uts unix.Utsname
			if err := unix.Uname(&uts); err != nil {
				return err
			}

			arch := unix.ByteSliceToString(uts.Machine[:])
			darwin := strings.ToLower(unix.ByteSliceToString(uts.Sysname[:]))
			macVersionNumber := unix.ByteSliceToString(uts.Release[:])

			cfg.NodeSpec.ObjectMeta.Labels["beta.kubernetes.io/arch"] = arch
			cfg.NodeSpec.ObjectMeta.Labels["kubernetes.io/arch"] = arch
			cfg.NodeSpec.ObjectMeta.Labels["beta.kubernetes.io/os"] = darwin
			cfg.NodeSpec.ObjectMeta.Labels["kubernetes.io/os"] = darwin
			delete(cfg.NodeSpec.ObjectMeta.Labels, "kubernetes.io/role")
			cfg.NodeSpec.Status.NodeInfo.Architecture = arch
			cfg.NodeSpec.Status.NodeInfo.OperatingSystem = darwin

			mid, err := machineid.ID()
			if err != nil {
				return err
			}
			mid = strings.ToLower(mid)

			cfg.NodeSpec.Status.NodeInfo.MachineID = mid
			cfg.NodeSpec.Status.NodeInfo.SystemUUID = mid
			cfg.NodeSpec.Status.NodeInfo.OSImage = "macOS " + macVersionNumber

			cfg.NodeSpec.Status.DaemonEndpoints.KubeletEndpoint = corev1.DaemonEndpoint{
				Port: int32(listenPort),
			}

			mem := sigar.Mem{}
			swap := sigar.Swap{}
			mem.Get()
			swap.Get()

			cfg.NodeSpec.Status.Capacity = corev1.ResourceList{
				"cpu":    *resource.NewQuantity(int64(runtime.NumCPU()), ""),
				"memory": *resource.NewQuantity(int64(mem.Total/1024), "Ki"),
				"pods":   *resource.NewQuantity(110, ""),
			}
			cfg.NodeSpec.Status.Allocatable = corev1.ResourceList{
				"cpu":    *resource.NewQuantity(int64(runtime.NumCPU()), ""),
				"memory": *resource.NewQuantity(int64(mem.Total/1024), "Ki"),
				"pods":   *resource.NewQuantity(110, ""),
			}

			cfg.NodeSpec.Status.Conditions = []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             corev1.ConditionTrue,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "KubeletReady",
					Message:            "kubelet is ready.",
				},
				{
					Type:               corev1.NodeMemoryPressure,
					Status:             corev1.ConditionFalse,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "KubeletHasSufficientMemory",
					Message:            "kubelet has sufficient memory available",
				},
				{
					Type:               corev1.NodeDiskPressure,
					Status:             corev1.ConditionFalse,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "KubeletHasNoDiskPressure",
					Message:            "kubelet has no disk pressure",
				},
				{
					Type:               corev1.NodeNetworkUnavailable,
					Status:             corev1.ConditionFalse,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "RouteCreated",
					Message:            "RouteController created a route",
				},
			}

			ifaces, err := net.Interfaces()
			if err == nil {
				for _, i := range ifaces {
					addrs, err := i.Addrs()
					if err == nil {
						for _, addr := range addrs {
							if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
								if ipnet.IP.To4() != nil && !ipnet.IP.IsLinkLocalMulticast() && !ipnet.IP.IsLinkLocalUnicast() {
									cfg.NodeSpec.Status.Addresses = append(cfg.NodeSpec.Status.Addresses, corev1.NodeAddress{
										Type:    corev1.NodeInternalIP,
										Address: ipnet.IP.String(),
									})
								}
							}
						}
					}
				}
			}

			return nil
		},
		withProviderID,
		withVersion,
		nodeutil.WithTLSConfig(nodeutil.WithKeyPairFromPath(certPath, keyPath), withCA),
		configureRoutes,
		func(cfg *nodeutil.NodeConfig) error {
			cfg.InformerResyncPeriod = resync
			cfg.NumWorkers = numberOfWorkers
			cfg.HTTPListenAddr = fmt.Sprintf(":%d", listenPort)
			return nil
		},
	)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- node.Run(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("error running the node: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := node.WaitReady(ctx, startupTimeout); err != nil {
		return fmt.Errorf("error waiting for node to be ready: %w", err)
	}

	<-node.Done()
	return node.Err()
}
