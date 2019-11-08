package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Until this is fixed: https://github.com/golang/protobuf/issues/748
// we need to change import path for every go repo.
func main() {
	submoduleDir := flag.String("submodule_dir", "", "directory where submodule lives")
	flag.Parse()

	if *submoduleDir == "" {
		flag.Usage()
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	goModPath, backPath, err := findGoMod(wd)
	if err != nil {
		log.Fatal(err)
	}

	moduleName, err := parseModuleName(goModPath)
	if err != nil {
		log.Fatal(err)
	}

	submoduleName, err := parseModuleName(path.Join(*submoduleDir, "go.mod"))
	if err != nil {
		log.Fatal(err)
	}

	oldImport := submoduleName
	newImport := path.Join(moduleName, backPath, *submoduleDir)

	numFilesFixed := 0
	err = filepath.Walk(*submoduleDir, func(filePath string, info os.FileInfo, err error) error {
		if !info.IsDir() && (strings.HasSuffix(filePath, "pb.go") || strings.HasSuffix(filePath, "pb.yarpc.go")) {
			err := replaceImport(filePath, oldImport, newImport)
			if err != nil {
				log.Fatal(err)
			} else {
				numFilesFixed++
			}
		}

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("fixed imports in %d files\n", numFilesFixed)
}

func findGoMod(wd string) (string, string, error) {
	dirCandidate := wd
	for dirCandidate != "/" {
		goModCandidate := path.Join(dirCandidate, "go.mod")
		if _, err := os.Stat(goModCandidate); err == nil {
			return goModCandidate, strings.Replace(wd, dirCandidate, "", 1), nil
		}
		dirCandidate = path.Dir(dirCandidate)
	}

	return "", "", fmt.Errorf("unable find go.mod in %s and its parents", wd)
}

func parseModuleName(goModPath string) (string, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "module") {
			moduleLine := strings.Split(line, " ")
			if len(moduleLine) > 1 {
				return moduleLine[1], nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", errors.New("")
}

func replaceImport(goFilePath string, oldImportPrefix string, newImportPrefix string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, goFilePath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	needWrite := false
	for _, importLine := range node.Imports {
		trimmedImportLine := strings.Trim(importLine.Path.Value, "\"")
		if strings.HasPrefix(trimmedImportLine, oldImportPrefix) {
			importLine.Path.Value = fmt.Sprintf("\"%s%s\"", newImportPrefix, trimmedImportLine[len(oldImportPrefix):])
			needWrite = true
		}
	}

	if needWrite {
		dst, err := os.Create(goFilePath)
		if err != nil {
			return err
		}
		defer dst.Close()

		err = format.Node(dst, fset, node)
		if err != nil {
			return err
		}
	}

	return nil
}
