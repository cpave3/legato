package dockerproxy

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// The fields a request body may have. Anything else is refused: a field
// added to the API later reaches the daemon only once it is listed here.
var (
	containerFields = []string{
		"Hostname", "Domainname", "User", "AttachStdin", "AttachStdout", "AttachStderr", "ExposedPorts", "Tty",
		"OpenStdin", "StdinOnce", "Env", "Cmd", "Healthcheck", "ArgsEscaped", "Image", "Volumes", "WorkingDir",
		"Entrypoint", "NetworkDisabled", "MacAddress", "OnBuild", "Labels", "StopSignal", "StopTimeout", "Shell",
		"HostConfig", "NetworkingConfig",
	}
	hostConfigFields = []string{
		"Binds", "ContainerIDFile", "LogConfig", "NetworkMode", "PortBindings", "RestartPolicy", "AutoRemove",
		"VolumeDriver", "VolumesFrom", "ConsoleSize", "Annotations", "CapAdd", "CapDrop", "CgroupnsMode", "Dns",
		"DnsOptions", "DnsSearch", "ExtraHosts", "GroupAdd", "IpcMode", "Cgroup", "Links", "OomScoreAdj", "PidMode",
		"Privileged", "PublishAllPorts", "ReadonlyRootfs", "SecurityOpt", "StorageOpt", "Tmpfs", "UTSMode",
		"UsernsMode", "ShmSize", "Sysctls", "Runtime", "Isolation", "Mounts", "MaskedPaths", "ReadonlyPaths", "Init",
		// Resources.
		"CpuShares", "Memory", "NanoCpus", "CgroupParent", "BlkioWeight", "BlkioWeightDevice", "BlkioDeviceReadBps",
		"BlkioDeviceWriteBps", "BlkioDeviceReadIOps", "BlkioDeviceWriteIOps", "CpuPeriod", "CpuQuota",
		"CpuRealtimePeriod", "CpuRealtimeRuntime", "CpusetCpus", "CpusetMems", "Devices", "DeviceCgroupRules",
		"DeviceRequests", "KernelMemory", "KernelMemoryTCP", "MemoryReservation", "MemorySwap", "MemorySwappiness",
		"OomKillDisable", "PidsLimit", "Ulimits", "CpuCount", "CpuPercent", "IOMaximumIOps", "IOMaximumBandwidth",
	}
	updateFields = []string{
		"CpuShares", "Memory", "NanoCpus", "BlkioWeight", "BlkioWeightDevice", "BlkioDeviceReadBps",
		"BlkioDeviceWriteBps", "BlkioDeviceReadIOps", "BlkioDeviceWriteIOps", "CpuPeriod", "CpuQuota",
		"CpuRealtimePeriod", "CpuRealtimeRuntime", "CpusetCpus", "CpusetMems", "KernelMemory", "KernelMemoryTCP",
		"MemoryReservation", "MemorySwap", "MemorySwappiness", "OomKillDisable", "PidsLimit", "Ulimits", "CpuCount",
		"CpuPercent", "IOMaximumIOps", "IOMaximumBandwidth", "RestartPolicy",
	}
	mountFields        = []string{"Type", "Source", "Target", "ReadOnly", "Consistency", "BindOptions", "VolumeOptions", "TmpfsOptions", "ImageOptions"}
	bindOptionFields   = []string{"Propagation", "NonRecursive", "CreateMountpoint", "ReadOnlyNonRecursive", "ReadOnlyForceRecursive"}
	volumeOptionFields = []string{"NoCopy", "Labels", "DriverConfig", "Subpath"}
	driverConfigFields = []string{"Name", "Options"}
	endpointFields     = []string{
		"IPAMConfig", "Links", "Aliases", "MacAddress", "DriverOpts", "GwPriority", "NetworkID", "EndpointID",
		"Gateway", "IPAddress", "IPPrefixLen", "IPv6Gateway", "GlobalIPv6Address", "GlobalIPv6PrefixLen", "DNSNames",
	}
	execFields    = []string{"AttachStdin", "AttachStdout", "AttachStderr", "ConsoleSize", "DetachKeys", "Tty", "Env", "Cmd", "Privileged", "User", "WorkingDir"}
	volumeFields  = []string{"Name", "Driver", "DriverOpts", "Labels"}
	networkFields = []string{"Name", "CheckDuplicate", "Driver", "Scope", "EnableIPv4", "EnableIPv6", "IPAM", "Internal", "Attachable", "Ingress", "ConfigOnly", "Options", "Labels"}
)

// Sysctls a container may set: those Linux keeps per namespace, so they
// cannot reach the host (the network's apply to the container's own
// network, as a host network is refused).
var namespacedSysctls = []string{"kernel.shm", "kernel.msg", "kernel.sem", "fs.mqueue.", "net."}

func (p *Proxy) createContainer(ctx context.Context, r *http.Request) error {
	body, err := readObject(r)
	if err != nil {
		return err
	}
	if err := body.normalize(containerFields, ""); err != nil {
		return err
	}
	hc, err := body.child("HostConfig")
	if err != nil {
		return err
	}
	if err := hc.normalize(hostConfigFields, "HostConfig."); err != nil {
		return err
	}
	if err := p.checkHostConfig(ctx, hc); err != nil {
		return err
	}
	if err := body.set("HostConfig", hc); err != nil {
		return err
	}
	nc, err := body.child("NetworkingConfig")
	if err != nil {
		return err
	}
	if err := nc.normalize([]string{"EndpointsConfig"}, "NetworkingConfig."); err != nil {
		return err
	}
	endpoints := map[string]json.RawMessage{}
	if _, err := nc.decode("EndpointsConfig", &endpoints); err != nil {
		return err
	}
	checked := map[string]*object{}
	for name, raw := range endpoints {
		if err := p.checkNetworkRef(ctx, name); err != nil {
			return err
		}
		endpoint, err := parseObject(raw)
		if err != nil {
			return refuse("NetworkingConfig.EndpointsConfig: %v", err)
		}
		if endpoint == nil {
			endpoint = &object{}
		}
		if err := p.checkEndpoint(ctx, endpoint); err != nil {
			return err
		}
		checked[name] = endpoint
	}
	if len(checked) > 0 {
		if err := nc.set("EndpointsConfig", checked); err != nil {
			return err
		}
		if err := body.set("NetworkingConfig", nc); err != nil {
			return err
		}
	}
	if err := p.labels(body, "Labels"); err != nil {
		return err
	}
	return setBody(r, body)
}

func (p *Proxy) checkHostConfig(ctx context.Context, hc *object) error {
	var privileged bool
	if _, err := hc.decode("Privileged", &privileged); err != nil {
		return err
	}
	if privileged {
		return refuse("privileged containers are not allowed: they are root on the host")
	}
	for _, f := range []string{"Devices", "DeviceCgroupRules", "DeviceRequests"} {
		var list []json.RawMessage
		if _, err := hc.decode(f, &list); err != nil {
			return err
		}
		if len(list) > 0 {
			return refuse("host devices are not allowed (HostConfig.%s)", f)
		}
	}
	for _, f := range []string{"MaskedPaths", "ReadonlyPaths"} {
		if hc.raw(f) != nil {
			return refuse("HostConfig.%s is not allowed: it would expose the host's /proc and /sys", f)
		}
	}
	var annotations map[string]string
	if _, err := hc.decode("Annotations", &annotations); err != nil {
		return err
	}
	if len(annotations) > 0 {
		return refuse("runtime annotations are not allowed")
	}
	var capAdd []string
	if _, err := hc.decode("CapAdd", &capAdd); err != nil {
		return err
	}
	for _, c := range capAdd {
		name := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(c)), "CAP_")
		if !slices.ContainsFunc(p.opts.CapAdd, func(a string) bool {
			return strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(a)), "CAP_") == name
		}) {
			return refuse("adding the capability %s is not allowed (agents.sandbox.docker.cap_add lists those that are)", name)
		}
	}
	for _, f := range []string{"PidMode", "IpcMode", "UTSMode", "UsernsMode", "CgroupnsMode"} {
		var mode string
		if _, err := hc.decode(f, &mode); err != nil {
			return err
		}
		if mode == "host" {
			return refuse("HostConfig.%s host is not allowed: it shares the host's namespace", f)
		}
		if strings.HasPrefix(mode, "container:") {
			if f != "PidMode" && f != "IpcMode" {
				return refuse("HostConfig.%s %s is not allowed", f, mode)
			}
			id, err := p.ownContainer(ctx, strings.TrimPrefix(mode, "container:"))
			if err != nil {
				return err
			}
			if err := hc.set(f, "container:"+id); err != nil {
				return err
			}
		}
	}
	var mode string
	if _, err := hc.decode("NetworkMode", &mode); err != nil {
		return err
	}
	switch {
	case mode == "host":
		return refuse("the host network is not allowed")
	case strings.HasPrefix(mode, "container:"):
		id, err := p.ownContainer(ctx, strings.TrimPrefix(mode, "container:"))
		if err != nil {
			return err
		}
		if err := hc.set("NetworkMode", "container:"+id); err != nil {
			return err
		}
	case mode != "":
		if err := p.checkNetworkRef(ctx, mode); err != nil {
			return err
		}
	}
	for _, f := range []string{"Cgroup", "CgroupParent"} {
		var v string
		if _, err := hc.decode(f, &v); err != nil {
			return err
		}
		if v != "" {
			return refuse("HostConfig.%s is not allowed", f)
		}
	}
	var securityOpt []string
	if _, err := hc.decode("SecurityOpt", &securityOpt); err != nil {
		return err
	}
	for _, opt := range securityOpt {
		switch strings.ToLower(strings.TrimSpace(opt)) {
		case "no-new-privileges", "no-new-privileges:true", "no-new-privileges=true":
		default:
			return refuse("the security option %q is not allowed: seccomp, AppArmor and SELinux stay as the daemon sets them", opt)
		}
	}
	var sysctls map[string]string
	if _, err := hc.decode("Sysctls", &sysctls); err != nil {
		return err
	}
	for key := range sysctls {
		if !slices.ContainsFunc(namespacedSysctls, func(prefix string) bool { return strings.HasPrefix(key, prefix) }) {
			return refuse("the sysctl %s is not allowed: only those kept per namespace are", key)
		}
	}
	var runtime, isolation, volumeDriver string
	for f, v := range map[string]*string{"Runtime": &runtime, "Isolation": &isolation, "VolumeDriver": &volumeDriver} {
		if _, err := hc.decode(f, v); err != nil {
			return err
		}
	}
	if !slices.Contains([]string{"", "runc", "crun", "runsc", "io.containerd.runc.v2"}, runtime) {
		return refuse("the runtime %s is not allowed", runtime)
	}
	if isolation != "" && isolation != "default" {
		return refuse("the isolation %s is not allowed", isolation)
	}
	if volumeDriver != "" && volumeDriver != "local" {
		return refuse("the volume driver %s is not allowed", volumeDriver)
	}
	if err := p.checkPorts(hc); err != nil {
		return err
	}
	for _, f := range []string{"Links", "VolumesFrom"} {
		var refs []string
		if _, err := hc.decode(f, &refs); err != nil {
			return err
		}
		for i, ref := range refs {
			name, rest, _ := strings.Cut(ref, ":")
			id, err := p.ownContainer(ctx, strings.TrimPrefix(name, "/"))
			if err != nil {
				return err
			}
			refs[i] = id
			if rest != "" {
				refs[i] += ":" + rest
			}
		}
		if len(refs) > 0 {
			if err := hc.set(f, refs); err != nil {
				return err
			}
		}
	}
	if err := p.checkBinds(ctx, hc); err != nil {
		return err
	}
	return p.checkMounts(ctx, hc)
}

// checkPorts makes published ports bind the loopback address unless they
// may bind any.
func (p *Proxy) checkPorts(hc *object) error {
	if p.opts.PublishAnywhere {
		return nil
	}
	var all bool
	if _, err := hc.decode("PublishAllPorts", &all); err != nil {
		return err
	}
	if all {
		return refuse("publishing every port (-P) is not allowed: publish each on 127.0.0.1, or set agents.sandbox.docker.publish to any")
	}
	bindings := map[string][]map[string]string{}
	found, err := hc.decode("PortBindings", &bindings)
	if err != nil || !found {
		return err
	}
	for port, list := range bindings {
		for i := range list {
			ip := net.ParseIP(list[i]["HostIp"])
			if ip == nil || !ip.IsLoopback() {
				list[i]["HostIp"] = "127.0.0.1"
			}
		}
		bindings[port] = list
	}
	return hc.set("PortBindings", bindings)
}

// checkBinds resolves each bind's source to the host path the sandbox
// shows, read-only where the sandbox shows it read-only, and checks each
// named volume is this agent's.
func (p *Proxy) checkBinds(ctx context.Context, hc *object) error {
	var binds []string
	found, err := hc.decode("Binds", &binds)
	if err != nil || !found {
		return err
	}
	for i, bind := range binds {
		parts := strings.Split(bind, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return refuse("the bind %q is not source:target[:options]", bind)
		}
		source := parts[0]
		var opts []string
		if len(parts) == 3 {
			opts = strings.Split(parts[2], ",")
		}
		if !strings.HasPrefix(source, "/") {
			if err := p.checkVolumeRef(ctx, source, nil); err != nil {
				return err
			}
			continue
		}
		host, readOnly, err := p.opts.View.Resolve(source)
		if err != nil {
			return refuse("the bind of %v", err)
		}
		var kept []string
		for _, o := range opts {
			switch o {
			case "rw", "ro":
				continue
			case "z", "Z":
				if readOnly {
					return refuse("relabelling %s (:%s) is not allowed: this agent may only read it", source, o)
				}
			case "shared", "rshared", "slave", "rslave":
				return refuse("the propagation %s is not allowed", o)
			}
			kept = append(kept, o)
		}
		if readOnly || slices.Contains(opts, "ro") {
			kept = append(kept, "ro")
		}
		binds[i] = host + ":" + parts[1]
		if len(kept) > 0 {
			binds[i] += ":" + strings.Join(kept, ",")
		}
	}
	return hc.set("Binds", binds)
}

func (p *Proxy) checkMounts(ctx context.Context, hc *object) error {
	mounts, err := hc.objects("Mounts")
	if err != nil || len(mounts) == 0 {
		return err
	}
	for _, m := range mounts {
		if err := m.normalize(mountFields, "HostConfig.Mounts[]."); err != nil {
			return err
		}
		var typ, source string
		var readOnly bool
		for f, v := range map[string]any{"Type": &typ, "Source": &source, "ReadOnly": &readOnly} {
			if _, err := m.decode(f, v); err != nil {
				return err
			}
		}
		switch typ {
		case "bind":
			if m.raw("VolumeOptions") != nil {
				return refuse("a bind mount takes no volume options")
			}
			host, hostReadOnly, err := p.opts.View.Resolve(source)
			if err != nil {
				return refuse("the bind of %v", err)
			}
			bindOpts, err := m.child("BindOptions")
			if err != nil {
				return err
			}
			if err := bindOpts.normalize(bindOptionFields, "BindOptions."); err != nil {
				return err
			}
			var propagation string
			var create, nonRecursiveRO bool
			for f, v := range map[string]any{"Propagation": &propagation, "CreateMountpoint": &create, "ReadOnlyNonRecursive": &nonRecursiveRO} {
				if _, err := bindOpts.decode(f, v); err != nil {
					return err
				}
			}
			if propagation != "" && propagation != "private" && propagation != "rprivate" {
				return refuse("the propagation %s is not allowed", propagation)
			}
			if create {
				return refuse("BindOptions.CreateMountpoint is not allowed: make %s in the sandbox first", source)
			}
			if hostReadOnly && nonRecursiveRO {
				return refuse("%s must be mounted read-only throughout", source)
			}
			if err := m.set("Source", host); err != nil {
				return err
			}
			if hostReadOnly {
				if err := m.set("ReadOnly", true); err != nil {
					return err
				}
			}
		case "volume":
			if m.raw("BindOptions") != nil {
				return refuse("a volume mount takes no bind options")
			}
			volOpts, err := m.child("VolumeOptions")
			if err != nil {
				return err
			}
			if err := volOpts.normalize(volumeOptionFields, "VolumeOptions."); err != nil {
				return err
			}
			driver, err := volOpts.child("DriverConfig")
			if err != nil {
				return err
			}
			if err := driver.normalize(driverConfigFields, "DriverConfig."); err != nil {
				return err
			}
			var name string
			var options map[string]string
			if _, err := driver.decode("Name", &name); err != nil {
				return err
			}
			if _, err := driver.decode("Options", &options); err != nil {
				return err
			}
			if name != "" && name != "local" {
				return refuse("the volume driver %s is not allowed", name)
			}
			checkedOpts, err := p.checkDriverOpts(options)
			if err != nil {
				return err
			}
			if source != "" {
				if err := p.checkVolumeRef(ctx, source, checkedOpts); err != nil {
					return err
				}
			} else if len(checkedOpts) > 0 {
				return refuse("an anonymous volume takes no driver options")
			}
			if len(options) > 0 {
				if err := driver.set("Options", checkedOpts); err != nil {
					return err
				}
				if err := volOpts.set("DriverConfig", driver); err != nil {
					return err
				}
				if err := m.set("VolumeOptions", volOpts); err != nil {
					return err
				}
			}
		case "tmpfs", "image":
			if m.raw("BindOptions") != nil || m.raw("VolumeOptions") != nil {
				return refuse("a %s mount takes no bind or volume options", typ)
			}
		default:
			return refuse("mounts of type %q are not allowed", typ)
		}
	}
	return hc.set("Mounts", mounts)
}

// checkDriverOpts allows a local volume that is a tmpfs or binds a path the
// sandbox shows (resolved, and read-only where the sandbox shows it so).
func (p *Proxy) checkDriverOpts(opts map[string]string) (map[string]string, error) {
	if len(opts) == 0 {
		return opts, nil
	}
	out := map[string]string{}
	for k, v := range opts {
		switch k {
		case "type", "o", "device", "size":
			out[k] = v
		default:
			return nil, refuse("the volume option %s is not allowed", k)
		}
	}
	var mountOpts []string
	if out["o"] != "" {
		mountOpts = strings.Split(out["o"], ",")
	}
	bind := slices.Contains(mountOpts, "bind") || slices.Contains(mountOpts, "rbind")
	switch {
	case bind:
		host, readOnly, err := p.opts.View.Resolve(out["device"])
		if err != nil {
			return nil, refuse("the volume's bind of %v", err)
		}
		out["device"] = host
		if readOnly {
			kept := []string{}
			for _, o := range mountOpts {
				if o != "rw" && o != "ro" {
					kept = append(kept, o)
				}
			}
			out["o"] = strings.Join(append(kept, "ro"), ",")
		}
	case out["type"] == "tmpfs":
	default:
		return nil, refuse("a volume may be a tmpfs, or bind a path the sandbox shows (o=bind,device=<path>); type %q device %q is not allowed", out["type"], out["device"])
	}
	return out, nil
}

// checkVolumeRef allows a named volume that is this agent's, and makes one
// that does not exist yet, labelled, so the daemon does not make it without
// the label.
func (p *Proxy) checkVolumeRef(ctx context.Context, name string, opts map[string]string) error {
	var v struct{ Labels map[string]string }
	status, err := p.upstreamJSON(ctx, http.MethodGet, "/volumes/"+url.PathEscape(name), nil, &v)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
		if !p.mine(v.Labels) {
			return refuse("the volume %s is not this agent's", name)
		}
		return nil
	case http.StatusNotFound:
		create := map[string]any{"Name": name, "Driver": "local", "Labels": map[string]string{Label: p.opts.Execution}}
		if len(opts) > 0 {
			create["DriverOpts"] = opts
		}
		status, err := p.upstreamJSON(ctx, http.MethodPost, "/volumes/create", create, nil)
		if err != nil {
			return err
		}
		if status/100 != 2 {
			return refuse("the volume %s could not be made (%d)", name, status)
		}
		return nil
	}
	return refuse("the volume %s could not be looked up (%d)", name, status)
}

// checkNetworkRef allows joining the default bridge or a network this agent
// made.
func (p *Proxy) checkNetworkRef(ctx context.Context, name string) error {
	if name == "host" {
		return refuse("the host network is not allowed")
	}
	if predefinedNetwork(name) {
		return nil
	}
	return p.ownNetwork(ctx, name)
}

func (p *Proxy) checkEndpoint(ctx context.Context, endpoint *object) error {
	if err := endpoint.normalize(endpointFields, "EndpointsConfig."); err != nil {
		return err
	}
	var links []string
	if _, err := endpoint.decode("Links", &links); err != nil {
		return err
	}
	for i, link := range links {
		name, alias, _ := strings.Cut(link, ":")
		id, err := p.ownContainer(ctx, strings.TrimPrefix(name, "/"))
		if err != nil {
			return err
		}
		links[i] = id
		if alias != "" {
			links[i] += ":" + alias
		}
	}
	if len(links) > 0 {
		return endpoint.set("Links", links)
	}
	return nil
}

func (p *Proxy) checkExec(r *http.Request) error {
	body, err := readObject(r)
	if err != nil {
		return err
	}
	if err := body.normalize(execFields, ""); err != nil {
		return err
	}
	var privileged bool
	if _, err := body.decode("Privileged", &privileged); err != nil {
		return err
	}
	if privileged {
		return refuse("privileged exec is not allowed")
	}
	return setBody(r, body)
}

func (p *Proxy) checkUpdate(r *http.Request) error {
	body, err := readObject(r)
	if err != nil {
		return err
	}
	if err := body.normalize(updateFields, ""); err != nil {
		return err
	}
	return setBody(r, body)
}

func (p *Proxy) checkCommit(ctx context.Context, r *http.Request) error {
	q := r.URL.Query()
	id, err := p.ownContainer(ctx, q.Get("container"))
	if err != nil {
		return err
	}
	q.Set("container", id)
	r.URL.RawQuery = q.Encode()
	body, err := readObject(r)
	if err != nil {
		return err
	}
	if err := body.normalize(containerFields, ""); err != nil {
		return err
	}
	if body.raw("HostConfig") != nil {
		return refuse("a commit takes no HostConfig")
	}
	if err := p.labels(body, "Labels"); err != nil {
		return err
	}
	return setBody(r, body)
}

func (p *Proxy) createVolume(r *http.Request) error {
	body, err := readObject(r)
	if err != nil {
		return err
	}
	if err := body.normalize(volumeFields, ""); err != nil {
		return err
	}
	var driver string
	var opts map[string]string
	if _, err := body.decode("Driver", &driver); err != nil {
		return err
	}
	if driver != "" && driver != "local" {
		return refuse("the volume driver %s is not allowed", driver)
	}
	if _, err := body.decode("DriverOpts", &opts); err != nil {
		return err
	}
	if len(opts) > 0 {
		checked, err := p.checkDriverOpts(opts)
		if err != nil {
			return err
		}
		if err := body.set("DriverOpts", checked); err != nil {
			return err
		}
	}
	if err := p.labels(body, "Labels"); err != nil {
		return err
	}
	return setBody(r, body)
}

// networkOptions are the driver options a network may set.
var networkOptions = []string{"com.docker.network.driver.mtu", "com.docker.network.bridge.enable_icc", "com.docker.network.enable_ipv6"}

func (p *Proxy) createNetwork(r *http.Request) error {
	body, err := readObject(r)
	if err != nil {
		return err
	}
	if err := body.normalize(networkFields, ""); err != nil {
		return err
	}
	var driver, scope string
	var ingress, configOnly bool
	var options map[string]string
	for f, v := range map[string]any{"Driver": &driver, "Scope": &scope, "Ingress": &ingress, "ConfigOnly": &configOnly, "Options": &options} {
		if _, err := body.decode(f, v); err != nil {
			return err
		}
	}
	if driver != "" && driver != "bridge" {
		return refuse("the network driver %s is not allowed: only bridge networks", driver)
	}
	if scope != "" && scope != "local" {
		return refuse("the network scope %s is not allowed", scope)
	}
	if ingress || configOnly {
		return refuse("ingress and config-only networks are not allowed")
	}
	for k := range options {
		if !slices.Contains(networkOptions, k) {
			return refuse("the network option %s is not allowed", k)
		}
	}
	if err := p.labels(body, "Labels"); err != nil {
		return err
	}
	return setBody(r, body)
}

// checkBuild refuses a build on the host network, and labels the image so
// this agent may remove it.
func (p *Proxy) checkBuild(r *http.Request) error {
	q := r.URL.Query()
	for _, mode := range q["networkmode"] {
		switch mode {
		case "", "default", "bridge", "none":
		default:
			return refuse("building with the network %s is not allowed", mode)
		}
	}
	for _, remote := range q["remote"] {
		if remote != "" && remote != "client-session" && !strings.HasPrefix(remote, "https://") && !strings.HasPrefix(remote, "http://") &&
			!strings.HasPrefix(remote, "git://") && !strings.HasPrefix(remote, "git@") {
			return refuse("the build context %s is not allowed", remote)
		}
	}
	labels := map[string]string{}
	for _, raw := range q["labels"] {
		var more map[string]string
		if err := json.Unmarshal([]byte(raw), &more); err != nil {
			return refuse("labels: %v", err)
		}
		for k, v := range more {
			labels[k] = v
		}
	}
	labels[Label] = p.opts.Execution
	data, _ := json.Marshal(labels)
	q.Del("labels")
	q.Set("labels", string(data))
	r.URL.RawQuery = q.Encode()
	return nil
}
