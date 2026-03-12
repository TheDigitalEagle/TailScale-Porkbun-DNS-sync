package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os/exec"
	"sort"
	"strings"
)

type CLI struct {
	bin string
}

func NewCLI(bin string) *CLI {
	return &CLI{bin: bin}
}

type Node struct {
	Name string
	IPv4 netip.Addr
}

type StatusSource interface {
	Status(context.Context) ([]Node, error)
}

type statusResponse struct {
	Self *peerInfo            `json:"Self"`
	Peer map[string]*peerInfo `json:"Peer"`
}

type peerInfo struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
}

func (c *CLI) Status(ctx context.Context) ([]Node, error) {
	cmd := exec.CommandContext(ctx, c.bin, "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run tailscale status --json: %w", err)
	}
	return ParseStatus(output)
}

func ParseStatus(data []byte) ([]Node, error) {
	var resp statusResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode tailscale status: %w", err)
	}

	nodes := make([]Node, 0, len(resp.Peer)+1)
	if resp.Self != nil {
		if node, ok := parsePeer(resp.Self); ok {
			nodes = append(nodes, node)
		}
	}
	for _, peer := range resp.Peer {
		if node, ok := parsePeer(peer); ok {
			nodes = append(nodes, node)
		}
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})

	return dedupe(nodes), nil
}

func parsePeer(peer *peerInfo) (Node, bool) {
	if peer == nil {
		return Node{}, false
	}

	name := normalizeHostname(strings.Split(strings.TrimSuffix(peer.DNSName, "."), ".")[0])
	if name == "" {
		name = normalizeHostname(peer.HostName)
	}
	if name == "" {
		return Node{}, false
	}

	for _, rawIP := range peer.TailscaleIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(rawIP))
		if err != nil || !addr.Is4() {
			continue
		}
		return Node{Name: name, IPv4: addr}, true
	}

	return Node{}, false
}

func normalizeHostname(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.Trim(name, ".")
	if name == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		case r == '_' || r == ' ' || r == '.':
			b.WriteByte('-')
		}
	}

	cleaned := strings.Trim(b.String(), "-")
	if cleaned == "" {
		return ""
	}
	return cleaned
}

func dedupe(nodes []Node) []Node {
	seen := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		seen[node.Name] = node
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]Node, 0, len(names))
	for _, name := range names {
		result = append(result, seen[name])
	}
	return result
}
