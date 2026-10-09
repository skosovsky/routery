//go:build e2e

package infratest_test

import (
	"archive/zip"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const consumerVersion = "v0.0.1-infrastructure"

func TestE2EConsumerUsesAllReleaseModules(t *testing.T) {
	// Arrange: construct unpublished artifacts from the current source, without local replacements.
	root := repoRoot(t)
	modules := strings.Split(command(t, root, nil, "make", "--no-print-directory", "-s", "modules"), "\n")
	work := t.TempDir()
	proxy := filepath.Join(work, "proxy")
	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		paths = append(paths, command(t, filepath.Join(root, module), nil, "go", "list", "-m", "-f", "{{.Path}}"))
	}
	for index, module := range modules {
		buildModuleArtifact(t, root, work, proxy, module, paths[index], paths)
	}
	consumer := filepath.Join(work, "consumer")
	var manifest strings.Builder
	manifest.WriteString("module consumer.example/routery\n\ngo 1.27.2\n\nrequire (\n")
	for _, path := range paths {
		manifest.WriteString("\t" + path + " " + consumerVersion + "\n")
	}
	write(t, filepath.Join(consumer, "go.mod"), []byte(manifest.String()+")\n"))
	var imports strings.Builder
	for _, path := range paths {
		if path == "github.com/skosovsky/routery" || path == "github.com/skosovsky/routery/ext/redis" {
			continue
		}
		imports.WriteString(" _ " + strconv.Quote(path) + "\n")
	}
	program := strings.Replace(consumerProgram, "{{MODULE_IMPORTS}}", imports.String(), 1)
	write(t, filepath.Join(consumer, "main.go"), []byte(program))
	proxyURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}
	// Reuse third-party download bytes while keeping module resolution in a fresh cache.
	cache := command(t, root, nil, "go", "env", "GOMODCACHE")
	cacheURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(cache, "cache", "download"))}
	env := []string{
		"GOPROXY=" + proxyURL.String() + "," + cacheURL.String() + ",https://proxy.golang.org",
		"GOPRIVATE=",
		"GONOPROXY=none",
		"GONOSUMDB=github.com/skosovsky/routery*",
		"GOMODCACHE=" + filepath.Join(work, "modcache"),
		"GOPATH=" + filepath.Join(work, "gopath"),
		"GOFLAGS=-modcacherw",
	}
	// Act: resolve every module from artifacts, then compile and execute a consumer.
	command(t, consumer, env, "go", "mod", "tidy")
	graph := command(
		t,
		consumer,
		env,
		"go",
		"list",
		"-m",
		"-f",
		"{{.Path}} {{.Version}} {{if .Replace}}REPLACED{{end}}",
		"all",
	)
	command(t, consumer, env, "go", "build", "./...")
	command(t, consumer, env, "go", "run", ".")
	// Assert: every discovered library module resolves to the prepared version without replacement.
	for _, path := range paths {
		if !strings.Contains(graph, path+" "+consumerVersion+" ") {
			t.Fatalf("module absent from consumer graph: %s\n%s", path, graph)
		}
	}
	if strings.Contains(graph, "REPLACED") ||
		strings.Contains(string(read(t, filepath.Join(consumer, "go.mod"))), "replace") {
		t.Fatal("consumer used development replacements")
	}
}

func buildModuleArtifact(t *testing.T, root, work, proxy, module, path string, paths []string) {
	t.Helper()
	original := filepath.Join(root, module)
	prepared := prepareModuleManifest(t, original, filepath.Join(work, "manifests", module), paths)
	destination := filepath.Join(proxy, filepath.FromSlash(path), "@v")
	write(t, filepath.Join(destination, consumerVersion+".mod"), prepared)
	write(
		t,
		filepath.Join(destination, consumerVersion+".info"),
		[]byte(`{"Version":"`+consumerVersion+`","Time":"2026-10-09T00:00:00Z"}`),
	)
	write(t, filepath.Join(destination, "list"), []byte(consumerVersion+"\n"))
	writeModuleZip(t, filepath.Join(destination, consumerVersion+".zip"), original, path, prepared)
}

func prepareModuleManifest(t *testing.T, original, stage string, paths []string) []byte {
	t.Helper()
	write(t, filepath.Join(stage, "go.mod"), read(t, filepath.Join(original, "go.mod")))
	var manifest struct {
		Require []struct {
			Path string `json:"Path"`
		} `json:"Require"`
	}
	if err := json.Unmarshal([]byte(command(t, stage, nil, "go", "mod", "edit", "-json")), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, dependency := range paths {
		for _, requirement := range manifest.Require {
			if requirement.Path == dependency {
				command(t, stage, nil, "go", "mod", "edit", "-require="+dependency+"@"+consumerVersion)
			}
		}
		command(t, stage, nil, "go", "mod", "edit", "-dropreplace="+dependency)
	}
	return read(t, filepath.Join(stage, "go.mod"))
}

func writeModuleZip(t *testing.T, destination, original, path string, prepared []byte) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	err = filepath.WalkDir(original, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return addModuleEntry(archive, original, current, path, prepared, entry)
	})
	archiveErr := archive.Close()
	fileErr := file.Close()
	if err != nil || archiveErr != nil || fileErr != nil {
		t.Fatalf("module archive: walk=%v zip=%v file=%v", err, archiveErr, fileErr)
	}
}

func addModuleEntry(archive *zip.Writer, original, current, path string, prepared []byte, entry fs.DirEntry) error {
	relative, relErr := filepath.Rel(original, current)
	if relErr != nil || relative == "." {
		return relErr
	}
	if entry.IsDir() {
		if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" {
			return filepath.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return filepath.SkipDir
		}
		return nil
	}
	if !entry.Type().IsRegular() || entry.Name() == "go.work" || entry.Name() == "go.work.sum" {
		return nil
	}
	data, readErr := os.ReadFile(current)
	if readErr != nil {
		return readErr
	}
	if relative == "go.mod" {
		data = prepared
	}
	writer, createErr := archive.Create(path + "@" + consumerVersion + "/" + filepath.ToSlash(relative))
	if createErr != nil {
		return createErr
	}
	_, writeErr := writer.Write(data)
	return writeErr
}

const consumerProgram = `package main
import (
 "context"
 "github.com/skosovsky/routery"
 redis "github.com/skosovsky/routery/ext/redis"
{{MODULE_IMPORTS}}
)
var _ = redis.NewStringRouteHandler[int]
func main() {
 table := routery.NewBasicRouteTable[int,string]()
 table.Route("leaf",0,nil,func(routery.RouteCall[int])(routery.BasicRouteResult[string],error){return routery.BasicHandled("ok"),nil})
 compiled,err:=table.Build();if err!=nil{panic(err)}
 result,err:=compiled.Dispatch(context.Background(),0);if err!=nil||result.Payload!="ok"{panic("dispatch failed")}
}
`
