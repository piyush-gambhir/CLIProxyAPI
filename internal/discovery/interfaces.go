package discovery

import (
	"net"
	"strings"
)


var IgnoredInterfacePrefixes = []string{
	"docker",
	"veth",
	"utun",
	"tailscale",
	"wg",
	"tun",
	"tap",
	"br-",
	"cni",
	"flannel",
	"virbr",
	"vmnet",
	"vboxnet",
	"awdl",
	"llw",
}




func FilterInterfaces(include, exclude []string) ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var valid []net.Interface
	for _, iface := range all {

		if iface.Flags&net.FlagUp == 0 ||
			iface.Flags&net.FlagLoopback != 0 ||
			iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}


		if iface.Flags&net.FlagMulticast == 0 {
			continue
		}

		name := strings.ToLower(iface.Name)


		if len(exclude) > 0 && matchesAny(name, exclude) {
			continue
		}



		if len(include) == 0 {
			if isVirtualOrTunnel(name) || !isLikelyPhysicalLAN(name) {
				continue
			}
		}


		if len(include) > 0 && !matchesAny(name, include) {
			continue
		}


		addrs, errAddrs := iface.Addrs()
		if errAddrs != nil || len(addrs) == 0 {
			continue
		}

		hasValidIP := false
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
				hasValidIP = true
				break
			}
		}

		if hasValidIP {
			valid = append(valid, iface)
		}
	}

	return valid, nil
}

func isVirtualOrTunnel(name string) bool {
	for _, prefix := range IgnoredInterfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isLikelyPhysicalLAN(name string) bool {
	for _, prefix := range []string{
		"en", "eth", "em", "igb", "ix", "re", "wl", "wlan", "wifi", "wi-fi", "ethernet", "bond",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(name, prefix) {
				return true
			}
		} else if name == p {
			return true
		}
	}
	return false
}
