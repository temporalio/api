package oracle

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// compile compiles files with protoc and returns the descriptors and protoc's output.
func compile(includes, files []string) (*descriptorpb.FileDescriptorSet, string, error) {
	dir, err := os.MkdirTemp("", "oracle")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "set.binpb")
	args := []string{"--include_source_info", "--descriptor_set_out=" + out}
	for _, include := range includes {
		args = append(args, "-I"+include)
	}
	output, err := exec.Command("protoc", append(args, files...)...).CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("protoc: %w\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, "", err
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, set); err != nil {
		return nil, "", err
	}
	return set, string(output), nil
}

// readTree reads the files under root/prefix, keyed by their slash-separated path
// relative to root.
func readTree(root, prefix string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(prefix)), func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[path.Clean(filepath.ToSlash(relative))] = string(data)
		return nil
	})
	return files, err
}
