package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

type config struct {
	descriptorPath string
	verifyOnly     bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.descriptorPath, "descriptorPath", "../../descriptor_set.pb", "path to the proto descriptor set")
	flag.BoolVar(&cfg.verifyOnly, "verifyOnly", false,
		"don't write to the filesystem, just verify output has not changed")
	flag.Parse()

	serviceErr := generateService(cfg)
	if serviceErr != nil {
		log.Print(serviceErr)
	}

	interceptorErr := generateInterceptor(cfg)
	if interceptorErr != nil {
		log.Print(interceptorErr)
	}

	requestHeaderErr := generateRequestHeader(cfg)
	if requestHeaderErr != nil {
		log.Print(requestHeaderErr)
	}

	if serviceErr != nil || interceptorErr != nil || requestHeaderErr != nil {
		os.Exit(1)
	}
}

func commentOutLines(str string) (string, error) {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(str))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			lines = append(lines, "//\n")
		} else {
			lines = append(lines, fmt.Sprintf("// %s\n", line))
		}
	}
	lines = append(lines, "\n")

	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, ""), nil
}
