package handler

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type NetworkAddress struct {
	IP        string `json:"ip"`
	Interface string `json:"interface"`
	Type      string `json:"type"`
	Domain    string `json:"domain,omitempty"`
}

type ServerInfoResponse struct {
	Port        int              `json:"port"`
	Addresses   []NetworkAddress `json:"addresses"`
	Hostname    string           `json:"hostname"`
	PairingCode string           `json:"pairing_code,omitempty"`
}

func (h *Handler) GetServerInfo(w http.ResponseWriter, r *http.Request) {
	port := 0
	if p := os.Getenv("PORT"); p != "" {
		for _, c := range p {
			port = port*10 + int(c-'0')
		}
	}

	hostname, _ := os.Hostname()

	addrs := listNetworkAddresses()

	tsDomain := probeTailscaleDomain()
	if tsDomain != "" {
		for i := range addrs {
			if addrs[i].Type == "tailscale" {
				addrs[i].Domain = tsDomain
			}
		}
	}

	var pairingCode string
	if h.PairingStore != nil {
		pairingCode = h.PairingStore.Code()
	}

	writeJSON(w, http.StatusOK, ServerInfoResponse{
		Port:        port,
		Addresses:   addrs,
		Hostname:    hostname,
		PairingCode: pairingCode,
	})
}

func listNetworkAddresses() []NetworkAddress {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var result []NetworkAddress
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			// Skip IPv6 for simplicity — most LAN/VPN use cases are IPv4.
			if ip.To4() == nil {
				continue
			}

			addrType := classifyInterface(iface.Name, ip)
			result = append(result, NetworkAddress{
				IP:        ip.String(),
				Interface: iface.Name,
				Type:      addrType,
			})
		}
	}
	return result
}

// classifyInterface determines the address type based on interface name and IP range.
func classifyInterface(name string, ip net.IP) string {
	// Tailscale uses CGNAT range 100.64.0.0/10
	if isTailscaleIP(ip) {
		return "tailscale"
	}
	// tun/utun interfaces are typically VPN
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "tun") || strings.HasPrefix(lower, "utun") ||
		strings.HasPrefix(lower, "wg") || strings.HasPrefix(lower, "tailscale") {
		return "vpn"
	}
	return "lan"
}

func isTailscaleIP(ip net.IP) bool {
	// 100.64.0.0/10 — CGNAT range used by Tailscale
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return v4[0] == 100 && (v4[1]&0xC0) == 64
}

// probeTailscaleDomain attempts to get the Tailscale MagicDNS name by running
// `tailscale status --json`. Returns empty string on any failure.
func probeTailscaleDomain() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return ""
	}

	var status struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return ""
	}
	return strings.TrimSuffix(status.Self.DNSName, ".")
}
