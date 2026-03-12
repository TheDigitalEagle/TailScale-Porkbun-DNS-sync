package tailscale

import "testing"

func TestParseStatus(t *testing.T) {
	t.Parallel()

	const payload = `{
	  "Self": {
	    "HostName": "workstation",
	    "DNSName": "workstation.tailnet.ts.net.",
	    "TailscaleIPs": ["100.64.0.1", "fd7a:115c:a1e0::1"]
	  },
	  "Peer": {
	    "node-1": {
	      "HostName": "media-server",
	      "DNSName": "media-server.tailnet.ts.net.",
	      "TailscaleIPs": ["100.64.0.2"]
	    },
	    "node-2": {
	      "HostName": "tablet",
	      "DNSName": "tablet.tailnet.ts.net.",
	      "TailscaleIPs": ["fd7a:115c:a1e0::2", "100.64.0.3"]
	    },
	    "node-3": {
	      "HostName": "ipv6-only",
	      "DNSName": "ipv6-only.tailnet.ts.net.",
	      "TailscaleIPs": ["fd7a:115c:a1e0::3"]
	    }
	  }
	}`

	nodes, err := ParseStatus([]byte(payload))
	if err != nil {
		t.Fatalf("ParseStatus() error = %v", err)
	}

	if got, want := len(nodes), 3; got != want {
		t.Fatalf("len(nodes) = %d, want %d", got, want)
	}

	gotByName := make(map[string]string, len(nodes))
	for _, node := range nodes {
		gotByName[node.Name] = node.IPv4.String()
	}

	if got, want := gotByName["media-server"], "100.64.0.2"; got != want {
		t.Fatalf("media-server IPv4 = %q, want %q", got, want)
	}

	if got, want := gotByName["tablet"], "100.64.0.3"; got != want {
		t.Fatalf("tablet IPv4 = %q, want %q", got, want)
	}

	if got, want := gotByName["workstation"], "100.64.0.1"; got != want {
		t.Fatalf("workstation IPv4 = %q, want %q", got, want)
	}
}

func TestParseStatusPrefersDNSName(t *testing.T) {
	t.Parallel()

	const payload = `{
	  "Peer": {
	    "node-1": {
	      "HostName": "DEVICE-1234",
	      "DNSName": "workstation.tailnet.ts.net.",
	      "TailscaleIPs": ["100.64.0.2"]
	    }
	  }
	}`

	nodes, err := ParseStatus([]byte(payload))
	if err != nil {
		t.Fatalf("ParseStatus() error = %v", err)
	}

	if got, want := len(nodes), 1; got != want {
		t.Fatalf("len(nodes) = %d, want %d", got, want)
	}
	if got, want := nodes[0].Name, "workstation"; got != want {
		t.Fatalf("nodes[0].Name = %q, want %q", got, want)
	}
}

func TestNormalizeHostname(t *testing.T) {
	t.Parallel()

	if got, want := normalizeHostname("My Laptop.local"), "my-laptop-local"; got != want {
		t.Fatalf("normalizeHostname() = %q, want %q", got, want)
	}
}
