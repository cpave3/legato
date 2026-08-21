package planvalidate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	mermaidVersion  = "11.16.0"
	wiremarkVersion = "0.1.0"
)

var nodeHelper = []byte(`import mermaid from "mermaid";
import { parse as parseWiremark } from "@wiremark/core";

const input = JSON.parse(await new Promise((resolve, reject) => {
  let value = "";
  process.stdin.setEncoding("utf8");
  process.stdin.on("data", chunk => value += chunk);
  process.stdin.on("end", () => resolve(value));
  process.stdin.on("error", reject);
}));

const diagnostics = [];
const add = (block, message, line = 0, column = 0) => diagnostics.push({
  kind: block.kind, index: block.index, line, column, message
});
const errorLocation = error => {
  const loc = error?.hash?.loc ?? error?.loc;
  if (loc) return { line: loc.first_line ?? loc.line ?? 0, column: (loc.first_column ?? loc.col ?? -1) + 1 };
  const match = String(error?.message ?? error).match(/line\s+(\d+)/i);
  return { line: match ? Number(match[1]) : 0, column: 0 };
};
const visit = (nodes, callback) => {
  for (const node of nodes ?? []) {
    callback(node);
    visit(node.children, callback);
  }
};

for (const block of input.blocks) {
  if (block.kind === "mermaid") {
    try {
      await mermaid.parse(block.source);
    } catch (error) {
      const loc = errorLocation(error);
      add(block, String(error?.message ?? error), loc.line, loc.column);
    }
    continue;
  }
  try {
    const document = parseWiremark(block.source);
    for (const diagnostic of document.diagnostics ?? []) {
      add(block, diagnostic.message, diagnostic.loc?.line ?? 0, diagnostic.loc?.col ?? 0);
    }
    const frameIDs = new Set(document.frames.map(frame => frame.id).filter(Boolean));
    const lines = block.source.split(/\r?\n/);
    for (const frame of document.frames) visit(frame.children, node => {
      const target = lines[(node.line ?? 1) - 1]?.match(/(?:^|\s)to=#([^\s]+)/)?.[1];
      if (target && !frameIDs.has(target)) {
        add(block, 'unknown frame target "#' + target + '"', node.line ?? 0, 0);
      }
    });
  } catch (error) {
    const loc = errorLocation(error);
    add(block, String(error?.message ?? error), loc.line, loc.column);
  }
}
process.stdout.write(JSON.stringify({ diagnostics }));
`)

type Options struct {
	CacheDir   string
	NodeBinary string
	NPMBinary  string
	Run        func(name string, args []string, dir string, stdin []byte) ([]byte, error)
}

type DependencyError struct {
	Err error
}

func (e *DependencyError) Error() string { return e.Err.Error() }
func (e *DependencyError) Unwrap() error { return e.Err }

func Validate(source []byte, options Options) ([]Diagnostic, error) {
	blocks, diagnostics := Extract(source)
	if len(blocks) == 0 {
		return diagnostics, nil
	}
	cacheDir, err := ensureNodeCache(options)
	if err != nil {
		return nil, &DependencyError{Err: err}
	}
	request, err := json.Marshal(map[string]any{"blocks": blocks})
	if err != nil {
		return nil, err
	}
	node := options.NodeBinary
	if node == "" {
		node = "node"
	}
	output, err := runCommand(options, node, []string{filepath.Join(cacheDir, "validate.mjs")}, cacheDir, request)
	if err != nil {
		return nil, &DependencyError{Err: fmt.Errorf("run Node plan validator: %w", err)}
	}
	var response struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, &DependencyError{Err: fmt.Errorf("decode Node plan validator output: %w", err)}
	}
	for i := range response.Diagnostics {
		for _, block := range blocks {
			if block.Kind == response.Diagnostics[i].Kind && block.Index == response.Diagnostics[i].Index {
				if response.Diagnostics[i].Line > 0 {
					response.Diagnostics[i].Line += block.Line - 1
				} else {
					response.Diagnostics[i].Line = block.Line
				}
				break
			}
		}
	}
	return append(diagnostics, response.Diagnostics...), nil
}

func ensureNodeCache(options Options) (string, error) {
	root := options.CacheDir
	if root == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve user cache: %w", err)
		}
		root = filepath.Join(userCache, "legato", "plan-validator")
	}
	hash := sha256.Sum256(nodeHelper)
	key := fmt.Sprintf("v1-mermaid-%s-wiremark-%s-%s", mermaidVersion, wiremarkVersion, hex.EncodeToString(hash[:6]))
	cacheDir := filepath.Join(root, key)
	if cacheComplete(cacheDir) {
		return cacheDir, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create plan validator cache: %w", err)
	}
	temporary, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return "", fmt.Errorf("create plan validator cache: %w", err)
	}
	defer os.RemoveAll(temporary)
	packageJSON := fmt.Sprintf(`{"private":true,"type":"module","dependencies":{"mermaid":"%s","@wiremark/core":"%s"}}`, mermaidVersion, wiremarkVersion)
	if err := os.WriteFile(filepath.Join(temporary, "package.json"), []byte(packageJSON), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(temporary, "validate.mjs"), nodeHelper, 0o644); err != nil {
		return "", err
	}
	npm := options.NPMBinary
	if npm == "" {
		npm = "npm"
	}
	if _, err := runCommand(options, npm, []string{"install", "--ignore-scripts", "--no-audit", "--no-fund", "--package-lock=false"}, temporary, nil); err != nil {
		return "", fmt.Errorf("install mermaid@%s and @wiremark/core@%s with npm: %w", mermaidVersion, wiremarkVersion, err)
	}
	if !cacheComplete(temporary) {
		return "", errors.New("npm completed without installing the plan validator packages")
	}
	if err := os.Rename(temporary, cacheDir); err != nil {
		if cacheComplete(cacheDir) {
			return cacheDir, nil
		}
		return "", fmt.Errorf("publish plan validator cache: %w", err)
	}
	return cacheDir, nil
}

func cacheComplete(path string) bool {
	for _, relative := range []string{"validate.mjs", "node_modules/mermaid/package.json", "node_modules/@wiremark/core/package.json"} {
		if info, err := os.Stat(filepath.Join(path, relative)); err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func runCommand(options Options, name string, args []string, dir string, stdin []byte) ([]byte, error) {
	if options.Run != nil {
		return options.Run(name, args, dir, stdin)
	}
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
