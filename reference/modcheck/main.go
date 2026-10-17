// Command modcheck holds the release checks that need golang.org/x/mod.
// It lives in the development-only reference module so that the
// published module keeps no dependencies.
//
//	go run ./modcheck zip [DIR]              check the module zip of DIR (default ..)
//	go run ./modcheck tag TAG [DIR]          check a release tag against DIR/go.mod
//	go run ./modcheck notes TAG [CHANGELOG]  print the tag's CHANGELOG.md section
//
// zip runs the same checks as the Go command when it builds a module zip
// for the proxy: it lists the omitted files, fails on any invalid file
// (Windows-reserved or case-colliding names, files that are too large, a
// zip that is too large) and reports the zip size.
//
// tag accepts only canonical SemVer 2.0.0 tags with the leading v
// (vMAJOR.MINOR.PATCH[-PRERELEASE], no build metadata) and checks that a
// vN tag (N >= 2) matches a /vN module path.
//
// notes prints the "## [X.Y.Z...]" section for the tag. A pre-release tag
// without its own section falls back to the section of its base version
// (v1.2.0-rc.1 uses "## [1.2.0-rc.1]" if present, else "## [1.2.0]").
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/zip"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "modcheck:", err)
		os.Exit(1)
	}
}

func run(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: modcheck zip [DIR] | tag TAG [DIR] | notes TAG [CHANGELOG]")
	}
	arg := func(i int, def string) string {
		if i < len(args) {
			return args[i]
		}
		return def
	}
	switch args[0] {
	case "zip":
		return checkZip(w, arg(1, ".."))
	case "tag":
		if len(args) < 2 {
			return errors.New("usage: modcheck tag TAG [DIR]")
		}
		path, err := modulePath(arg(2, ".."))
		if err != nil {
			return err
		}
		if err := checkTag(args[1], path); err != nil {
			return err
		}
		fmt.Fprintf(w, "tag %s is valid for module %s\n", args[1], path)
		return nil
	case "notes":
		if len(args) < 2 {
			return errors.New("usage: modcheck notes TAG [CHANGELOG]")
		}
		data, err := os.ReadFile(arg(2, "../CHANGELOG.md"))
		if err != nil {
			return err
		}
		notes, err := releaseNotes(string(data), args[1])
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, notes)
		return err
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func modulePath(dir string) (string, error) {
	name := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	f, err := modfile.ParseLax(name, data, nil)
	if err != nil {
		return "", err
	}
	if f.Module == nil {
		return "", fmt.Errorf("%s: no module directive", name)
	}
	return f.Module.Mod.Path, nil
}

func checkZip(w io.Writer, dir string) error {
	path, err := modulePath(dir)
	if err != nil {
		return err
	}
	files, err := zip.CheckDir(dir)
	if err != nil && files.SizeError == nil && len(files.Invalid) == 0 {
		return err // not a file problem: I/O and similar
	}
	rel := func(p string) string {
		if r, err := filepath.Rel(dir, p); err == nil {
			return filepath.ToSlash(r)
		}
		return p
	}
	for _, f := range files.Omitted {
		fmt.Fprintf(w, "omitted: %s (%v)\n", rel(f.Path), f.Err)
	}
	for _, f := range files.Invalid {
		fmt.Fprintf(w, "INVALID: %s (%v)\n", rel(f.Path), f.Err)
	}
	if err := files.Err(); err != nil {
		return fmt.Errorf("module zip of %s is invalid: %w", dir, err)
	}

	// Build the zip as the Go command would, to catch what CheckDir does
	// not (for example an unreadable file), and report its size.
	tmp, err := os.CreateTemp("", "modcheck-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	// The version only names the zip's top directory; it must match the
	// major version suffix of the path (v0.0.0, or vN.0.0 for /vN).
	version := "v0.0.0"
	if _, major, ok := module.SplitPathVersion(path); ok && major != "" {
		version = strings.TrimPrefix(major, "/") + ".0.0"
	}
	if err := zip.CreateFromDir(tmp, module.Version{Path: path, Version: version}, dir); err != nil {
		return fmt.Errorf("create module zip: %w", err)
	}
	info, err := tmp.Stat()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "module %s: %d files, zip size %d bytes (limit %d)\n",
		path, len(files.Valid), info.Size(), int64(zip.MaxZipFile))
	return nil
}

func checkTag(tag, modPath string) error {
	if !semver.IsValid(tag) || semver.Canonical(tag) != tag {
		return fmt.Errorf("tag %q is not a SemVer version vMAJOR.MINOR.PATCH[-PRERELEASE] (no build metadata)", tag)
	}
	major := semver.Major(tag) // "v0", "v1", "v2", ...
	_, suffix, ok := module.SplitPathVersion(modPath)
	if !ok {
		return fmt.Errorf("invalid module path %q", modPath)
	}
	suffix = strings.TrimPrefix(suffix, "/")
	switch {
	case major == "v0" || major == "v1":
		if suffix != "" {
			return fmt.Errorf("tag %s needs a module path without a major suffix, go.mod has %s", tag, modPath)
		}
	case suffix != major:
		return fmt.Errorf("tag %s needs module path suffix /%s, go.mod has %s", tag, major, modPath)
	}
	return nil
}

// releaseNotes returns the body of the CHANGELOG section for tag, falling
// back from a pre-release to its base version.
func releaseNotes(changelog, tag string) (string, error) {
	if !semver.IsValid(tag) {
		return "", fmt.Errorf("invalid tag %q", tag)
	}
	version := strings.TrimPrefix(semver.Canonical(tag), "v")
	candidates := []string{version}
	if pre := semver.Prerelease(tag); pre != "" {
		candidates = append(candidates, strings.TrimSuffix(version, pre))
	}
	for _, v := range candidates {
		if body := section(changelog, "## ["+v+"]"); strings.TrimSpace(body) != "" {
			return body, nil
		}
	}
	return "", fmt.Errorf("CHANGELOG.md has no non-empty section %q", "## ["+strings.Join(candidates, "] or ## [")+"]")
}

func section(changelog, head string) string {
	var b strings.Builder
	found := false
	sc := bufio.NewScanner(strings.NewReader(changelog))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## [") {
			if found {
				break
			}
			if strings.HasPrefix(line, head) {
				found = true
				continue
			}
		}
		// Link reference definitions at the bottom end the last section.
		if found && strings.HasPrefix(line, "[") && strings.Contains(line, "]: ") {
			break
		}
		if found {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
