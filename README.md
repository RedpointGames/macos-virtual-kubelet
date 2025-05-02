# Virtual Kubelet for macOS Host Processes

This is a (very experimental) virtual kubelet for running processes directly on macOS. Processes are not run under virtualisation of any kind, but are instead run as launchd LaunchAgents as the current user. Thus there's no constraint on the number of "pods" that can be scheduled on each machine, but there's also no security or isolation between pods. You should consider pods running on these machines to be the equivalent of Windows node "HostProcess" containers.

## License / Attribution

This code is under an Apache-2.0 license. The basic scaffolding to get the virtual kubelet up and running in `main.go` is heavily based on https://github.com/agoda-com/macOS-vz-kubelet, which is also licensed under Apache-2.0.
