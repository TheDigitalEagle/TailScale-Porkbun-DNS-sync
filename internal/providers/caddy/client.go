package caddy

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"porkbun-dns/internal/model"
)

const managedMarker = "# managed-by: porkbun-dns"

type Client struct {
	path      string
	tlsImport string
}

type Route struct {
	Host           string
	Upstream       string
	TLSImport      string
	RootRedirectTo string
}

func NewClient(path, tlsImport string) *Client {
	return &Client{path: path, tlsImport: tlsImport}
}

func (c *Client) IngressRecords(context.Context) ([]model.Record, error) {
	content, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("read caddyfile: %w", err)
	}

	blocks := parseBlocks(string(content))
	records := make([]model.Record, 0, len(blocks))
	for _, block := range blocks {
		if block.Host == "" || block.Upstream == "" {
			continue
		}

		records = append(records, model.Record{
			Name:           block.Host,
			FQDN:           block.Host,
			Type:           "HTTP_PROXY",
			Scope:          "ingress",
			Owner:          owner(block.Managed),
			SourceOfTruth:  "caddy",
			Targets:        []string{"caddy"},
			Status:         "unmanaged",
			ObservedValues: []string{block.Upstream},
		})
	}

	sort.Slice(records, func(i, j int) bool { return records[i].FQDN < records[j].FQDN })
	return records, nil
}

func (c *Client) UpsertRoute(ctx context.Context, route Route) error {
	_ = ctx

	content, err := os.ReadFile(c.path)
	if err != nil {
		return fmt.Errorf("read caddyfile: %w", err)
	}

	text := string(content)
	blocks := parseBlocks(text)
	newBlock := renderManagedBlock(route, c.tlsImport)

	for _, block := range blocks {
		if block.Host != route.Host {
			continue
		}
		if !block.Managed {
			return fmt.Errorf("refusing to overwrite unmanaged caddy block for %s", route.Host)
		}
		text = replaceBlock(text, block.StartOffset, block.EndOffset, newBlock)
		return os.WriteFile(c.path, []byte(text), 0o644)
	}

	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "\n" + newBlock
	return os.WriteFile(c.path, []byte(text), 0o644)
}

func (c *Client) DeleteRoute(ctx context.Context, host string) error {
	_ = ctx

	content, err := os.ReadFile(c.path)
	if err != nil {
		return fmt.Errorf("read caddyfile: %w", err)
	}

	text := string(content)
	blocks := parseBlocks(text)
	for _, block := range blocks {
		if block.Host != host {
			continue
		}
		if !block.Managed {
			return fmt.Errorf("refusing to delete unmanaged caddy block for %s", host)
		}
		text = replaceBlock(text, block.StartOffset, block.EndOffset, "")
		return os.WriteFile(c.path, []byte(strings.TrimLeft(text, "\n")), 0o644)
	}

	return nil
}

type block struct {
	Host           string
	Upstream       string
	TLSImport      string
	RootRedirectTo string
	Managed        bool
	StartOffset    int
	EndOffset      int
}

func parseBlocks(content string) []block {
	lines := strings.SplitAfter(content, "\n")
	offsets := make([]int, len(lines))
	cursor := 0
	for i, line := range lines {
		offsets[i] = cursor
		cursor += len(line)
	}

	var blocks []block
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasSuffix(line, "{") {
			continue
		}

		header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		if header == "" || strings.HasPrefix(header, "(") || strings.Contains(header, " ") {
			continue
		}

		depth := strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		end := i
		for depth > 0 && end+1 < len(lines) {
			end++
			depth += strings.Count(lines[end], "{") - strings.Count(lines[end], "}")
		}

		startOffset := offsets[i]
		managed := false
		if i > 0 && strings.TrimSpace(lines[i-1]) == managedMarker {
			managed = true
			startOffset = offsets[i-1]
		}

		blockLines := lines[i : end+1]
		blocks = append(blocks, block{
			Host:           header,
			Upstream:       parseDirectiveValue(blockLines, "reverse_proxy"),
			TLSImport:      parseDirectiveValue(blockLines, "import"),
			RootRedirectTo: parseRedirect(blockLines),
			Managed:        managed,
			StartOffset:    startOffset,
			EndOffset:      offsets[end] + len(lines[end]),
		})

		i = end
	}

	return blocks
}

func parseDirectiveValue(lines []string, directive string) string {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, directive+" ") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(trimmed, directive))
	}
	return ""
}

func parseRedirect(lines []string) string {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "redir ") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) >= 3 {
			return parts[2]
		}
	}
	return ""
}

func renderManagedBlock(route Route, defaultTLSImport string) string {
	var b strings.Builder
	b.WriteString(managedMarker + "\n")
	b.WriteString(route.Host + " {\n")

	tlsImport := strings.TrimSpace(route.TLSImport)
	if tlsImport == "" {
		tlsImport = strings.TrimSpace(defaultTLSImport)
	}
	if tlsImport != "" {
		b.WriteString("  import " + tlsImport + "\n")
	}
	if strings.TrimSpace(route.RootRedirectTo) != "" {
		b.WriteString("  @root path /\n")
		b.WriteString("  redir @root " + strings.TrimSpace(route.RootRedirectTo) + " 308\n")
	}
	b.WriteString("  reverse_proxy " + strings.TrimSpace(route.Upstream) + "\n")
	b.WriteString("}\n")
	return b.String()
}

func replaceBlock(content string, start, end int, replacement string) string {
	return content[:start] + replacement + content[end:]
}

func owner(managed bool) string {
	if managed {
		return "desired-state"
	}
	return "provider-managed"
}
