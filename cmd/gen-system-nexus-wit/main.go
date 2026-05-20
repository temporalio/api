package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	exposedTagRE = regexp.MustCompile(`\btags\s*=\s*"exposed"`)
	packageRE    = regexp.MustCompile(`^\s*package\s+([A-Za-z0-9_.]+)\s*;`)
	rpcRE        = regexp.MustCompile(`^\s*rpc\s+([A-Za-z0-9_]+)\s*\(`)
	serviceRE    = regexp.MustCompile(`^\s*service\s+([A-Za-z0-9_]+)\s*\{`)
)

func main() {
	var descriptors string
	var nexusAPIGen string
	var output string
	flag.StringVar(&descriptors, "descriptors", "", "protobuf descriptor set")
	flag.StringVar(&nexusAPIGen, "nexus-api-gen", "nexus-api-gen", "path to nexus-api-gen")
	flag.StringVar(&output, "output", "", "output WIT file")
	flag.Parse()

	if descriptors == "" || output == "" || flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "usage: gen_system_nexus_wit --descriptors DESCRIPTORS [--nexus-api-gen PATH] --output OUTPUT PROTO...\n")
		os.Exit(2)
	}

	rpcs, err := exposedRPCs(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(rpcs) == 0 {
		fmt.Fprintln(os.Stderr, "no proto RPCs are marked as exposed Nexus operations")
		os.Exit(1)
	}

	tempDir, err := os.MkdirTemp("", "system-nexus-wit-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	tempOutput := filepath.Join(tempDir, "system-nexus.wit")
	var input string
	if _, err := os.Stat(output); err == nil {
		if err := copyFile(output, tempOutput); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		input = tempOutput
	} else if !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, rpc := range rpcs {
		if err := runAddRPC(nexusAPIGen, descriptors, rpc, tempOutput, input); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		input = tempOutput
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := copyFile(tempOutput, output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func exposedRPCs(paths []string) ([]string, error) {
	var rpcs []string
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		var packageName string
		var serviceName string
		var serviceDepth int
		var rpcName string
		var rpcDepth int
		var rpcIsExposed bool

		for _, rawLine := range strings.Split(string(content), "\n") {
			line := stripLineComment(rawLine)

			if packageName == "" {
				if match := packageRE.FindStringSubmatch(line); match != nil {
					packageName = match[1]
				}
			}

			if serviceName == "" {
				if match := serviceRE.FindStringSubmatch(line); match != nil {
					serviceName = match[1]
					serviceDepth = braceDelta(line)
				}
				continue
			}

			if rpcName == "" {
				if match := rpcRE.FindStringSubmatch(line); match != nil {
					rpcName = match[1]
					rpcDepth = braceDelta(line)
					rpcIsExposed = exposedTagRE.MatchString(line)
					if rpcDepth <= 0 {
						if rpcIsExposed {
							rpcs = append(rpcs, qualifiedRPCName(path, packageName, serviceName, rpcName))
						}
						rpcName = ""
						rpcDepth = 0
						rpcIsExposed = false
					}
					continue
				}

				serviceDepth += braceDelta(line)
				if serviceDepth <= 0 {
					serviceName = ""
					serviceDepth = 0
				}
				continue
			}

			rpcIsExposed = rpcIsExposed || exposedTagRE.MatchString(line)
			rpcDepth += braceDelta(line)
			if rpcDepth <= 0 {
				if rpcIsExposed {
					rpcs = append(rpcs, qualifiedRPCName(path, packageName, serviceName, rpcName))
				}
				rpcName = ""
				rpcDepth = 0
				rpcIsExposed = false
			}
		}
	}
	return rpcs, nil
}

func qualifiedRPCName(path string, packageName string, serviceName string, rpcName string) string {
	if packageName == "" || serviceName == "" {
		fmt.Fprintf(os.Stderr, "%s: exposed RPC has no package or service\n", path)
		os.Exit(1)
	}
	return packageName + "." + serviceName + "." + rpcName
}

func runAddRPC(nexusAPIGen string, descriptors string, rpc string, output string, input string) error {
	args := []string{
		"add-rpc",
		"--descriptors", descriptors,
		"--rpc", rpc,
		"--output", output,
	}
	if input != "" {
		args = append(args, "--input", input)
	}

	command := exec.Command(nexusAPIGen, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", nexusAPIGen, strings.Join(args, " "), err)
	}
	return nil
}

func copyFile(source string, destination string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, content, 0o644)
}

func stripLineComment(line string) string {
	if before, _, ok := strings.Cut(line, "//"); ok {
		return before
	}
	return line
}

func braceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
}
