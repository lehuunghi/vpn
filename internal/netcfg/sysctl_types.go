package netcfg

// Sysctl is one kernel parameter and the value vpn20 wants for it.
type Sysctl struct {
	Key   string
	Value string
	// Required marks the ones the VPN cannot work without (forwarding). The
	// others are throughput tuning: nice to have, and often refused inside a
	// container because they are not network-namespaced.
	Required bool
	// Why is shown in the log and the UI when a value could not be set.
	Why string
}

// Result records what happened to one sysctl.
type Result struct {
	Sysctl
	Applied bool
	Current string
	Err     string
}
