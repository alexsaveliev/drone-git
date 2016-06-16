package main

import (
	"bufio"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/drone/drone-plugin-go/plugin"
)

// commits is a list of commits of different types (push, pull request, tag)
// to help us verify that this clone plugin can handle multiple commit types.
var commits = []struct {
	path             string
	clone            string
	event            string
	branch           string
	commit           string
	ref              string
	file             string
	data             string
	tags             []string
	submodules       map[string]string
	tweak            bool
	skipNotEmptyTest bool
}{
	// first commit
	{
		path:       "octocat/Hello-World",
		clone:      "https://github.com/octocat/Hello-World.git",
		event:      plugin.EventPush,
		branch:     "master",
		commit:     "553c2077f0edc3d5dc5d17262f6aa498e69d6f8e",
		ref:        "refs/heads/master",
		file:       "README",
		data:       "Hello World!",
		tags:       nil,
		submodules: nil,
	},
	// head commit
	{
		path:       "octocat/Hello-World",
		clone:      "https://github.com/octocat/Hello-World.git",
		event:      plugin.EventPush,
		branch:     "master",
		commit:     "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d",
		ref:        "refs/heads/master",
		file:       "README",
		data:       "Hello World!\n",
		tags:       nil,
		submodules: nil,
	},
	// pull request commit
	{
		path:       "octocat/Hello-World",
		clone:      "https://github.com/octocat/Hello-World.git",
		event:      plugin.EventPull,
		branch:     "master",
		commit:     "553c2077f0edc3d5dc5d17262f6aa498e69d6f8e",
		ref:        "refs/pull/208/merge",
		file:       "README",
		data:       "Goodbye World!\n",
		tags:       nil,
		submodules: nil,
	},
	// branch
	{
		path:       "octocat/Hello-World",
		clone:      "https://github.com/octocat/Hello-World.git",
		event:      plugin.EventPush,
		branch:     "test",
		commit:     "b3cbd5bbd7e81436d2eee04537ea2b4c0cad4cdf",
		ref:        "refs/heads/test",
		file:       "CONTRIBUTING.md",
		data:       "## Contributing\n",
		tags:       nil,
		submodules: nil,
	},
	// tags
	{
		path:   "github/mime-types",
		clone:  "https://github.com/github/mime-types.git",
		event:  plugin.EventTag,
		branch: "master",
		commit: "553c2077f0edc3d5dc5d17262f6aa498e69d6f8e",
		ref:    "refs/tags/v1.17",
		file:   ".gitignore",
		data:   "*.swp\n*~\n.rake_tasks~\nhtml\ndoc\npkg\npublish\ncoverage\n",
		tags: []string{
			"v1.16",
			"v1.17",
			"v1.17.1",
			"v1.17.2",
			"v1.18",
			"v1.19",
			"v1.20",
			"v1.20.1",
			"v1.21",
			"v1.22",
			"v1.23",
		},
		submodules: nil,
	},
	// submodules
	{
		path:   "msteinert/drone-git-test-submodule",
		clone:  "https://github.com/msteinert/drone-git-test-submodule.git",
		event:  plugin.EventPush,
		branch: "master",
		commit: "072ae3ddb6883c8db653f8d4432b07c035b93753",
		ref:    "refs/heads/master",
		file:   "Hello-World/README",
		data:   "Hello World!\n",
		tags:   nil,
		submodules: map[string]string{
			"Hello-World": "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d",
		},
	},
	// tweak attributes (eol=crlf)
	{
		path:       "sgtest/gitattributes",
		clone:      "https://github.com/sgtest/gitattributes.git",
		event:      plugin.EventPush,
		branch:     "master",
		commit:     "4d003e20c18ce15a69eeac67b3fdb94932d35271",
		ref:        "refs/heads/master",
		file:       "crlf.go",
		data:       "package main\n\nfunc bar() {\n\tbar()\n}",
		tags:       nil,
		submodules: nil,
		tweak:      true,
	},
	// tweak attributes (ident)
	{
		path:       "sgtest/gitattributes",
		clone:      "https://github.com/sgtest/gitattributes.git",
		event:      plugin.EventPush,
		branch:     "master",
		commit:     "4d003e20c18ce15a69eeac67b3fdb94932d35271",
		ref:        "refs/heads/master",
		file:       "ident.go",
		data:       "package main\n\nvar qux = \"$Id$\"",
		tags:       nil,
		submodules: nil,
		tweak:      true,
	},
	// tweak attributes (disabled)
	{
		path:       "sgtest/gitattributes",
		clone:      "https://github.com/sgtest/gitattributes.git",
		event:      plugin.EventPush,
		branch:     "master",
		commit:     "4d003e20c18ce15a69eeac67b3fdb94932d35271",
		ref:        "refs/heads/master",
		file:       "ident.go",
		data:       "package main\r\n\r\nvar qux = \"$Id: b1a36a842cd537057c6214ba858fe16654cc0935 $\"",
		tags:       nil,
		submodules: nil,
		// you can't change "TweakGitattributes" while checking in into the same directory
		skipNotEmptyTest: true,
	},
	// empty ref
	{
		path:       "apache/log4j",
		clone:      "https://github.com/apache/log4j.git",
		event:      plugin.EventPush,
		branch:     "BRANCH_1_3_ABANDONED",
		commit:     "c5f4279081091e562c44bb753f6e52dc6be5fa52",
		ref:        "",
		file:       "NOTICE",
		data:       "This product includes software developed by\nThe Apache Software Foundation (http://www.apache.org/).\n",
		tags:       nil,
		submodules: nil,
	},
}

// TestClone tests the ability to clone a specific commit into
// a fresh, empty directory every time.
func TestClone(t *testing.T) {

	for _, c := range commits {
		dir := setup()

		recursive := false
		if c.submodules != nil {
			recursive = true
		}

		tags := false
		if c.tags != nil {
			tags = true
		}

		r := &plugin.Repo{Clone: c.clone}
		b := &plugin.Build{Commit: c.commit, Branch: c.branch, Ref: c.ref, Event: c.event}
		w := &plugin.Workspace{Path: dir}
		v := &Params{
			Recursive:          recursive,
			Tags:               tags,
			TweakGitattributes: c.tweak,
		}
		if err := clone(r, b, w, v); err != nil {
			t.Errorf("Expected successful clone. Got error. %s.", err)
		}

		fileData := readFile(dir, c.file)
		if fileData != c.data {
			t.Errorf("Expected %s to contain [%s]. Got [%s].", c.file, c.data, fileData)
		}

		if c.tweak {
			cmd := exec.Command("git", "show", c.commit+":"+c.file)
			cmd.Dir = dir
			data, err := cmd.CombinedOutput()
			if err != nil {
				t.Error(err)
			}
			gitData := string(data)
			if gitData != fileData {
				t.Errorf("Expected file %s to contain raw data [%s]. Got [%s].", c.file, gitData, fileData)
			}
		}

		if c.tags != nil {
			tags, err := getTags(dir)
			if err != nil {
				t.Error(err)
			}
			for _, tag := range c.tags {
				if !tags[tag] {
					t.Errorf("Expected tag [%s] to exist.", tag)
				}
			}
		}

		if c.submodules != nil {
			submodules, err := getSubmodules(dir)
			if err != nil {
				t.Error(err)
			}
			for k, v := range c.submodules {
				if submodules[k] != v {
					t.Errorf("Expected submodule [%s:%s] to exist.", k, v)
				}
			}
		}

		teardown(dir)
	}
}

// TestCloneNonEmpty tests the ability to clone a specific commit into
// a non-empty directory. This is useful if the git workspace is cached
// and re-stored for every build.
func TestCloneNonEmpty(t *testing.T) {
	dir := setup()
	defer teardown(dir)

	for _, c := range commits {

		if c.skipNotEmptyTest {
			continue
		}

		recursive := false
		if c.submodules != nil {
			recursive = true
		}

		tags := false
		if c.tags != nil {
			tags = true
		}

		r := &plugin.Repo{Clone: c.clone}
		b := &plugin.Build{Commit: c.commit, Branch: c.branch, Ref: c.ref, Event: c.event}
		w := &plugin.Workspace{Path: filepath.Join(dir, c.path)}
		v := &Params{
			Recursive:          recursive,
			Tags:               tags,
			TweakGitattributes: c.tweak,
		}
		if err := clone(r, b, w, v); err != nil {
			t.Errorf("Expected successful clone. Got error. %s.", err)
			break
		}

		data := readFile(w.Path, c.file)
		if data != c.data {
			t.Errorf("Expected %s to contain [%s]. Got [%s].", c.file, c.data, data)
			break
		}

		if c.tags != nil {
			tags, err := getTags(w.Path)
			if err != nil {
				t.Error(err)
			}
			for _, tag := range c.tags {
				if !tags[tag] {
					t.Errorf("Expected tag [%s] to exist.", tag)
				}
			}
		}

		if c.submodules != nil {
			submodules, err := getSubmodules(w.Path)
			if err != nil {
				t.Error(err)
			}
			for k, v := range c.submodules {
				if submodules[k] != v {
					t.Errorf("Expected submodule [%s:%s] to exist.", k, v)
				}
			}
		}
	}
}

// TestClone tests if the arguments to `git fetch` are constructed properly.
func TestFetch(t *testing.T) {
	testdata := []struct {
		build *plugin.Build
		tags  bool
		depth int
		exp   []string
	}{
		{
			&plugin.Build{Ref: "refs/heads/master"},
			false,
			0,
			[]string{
				"git",
				"fetch",
				"--no-tags",
				"origin",
				"+refs/heads/master:",
			},
		},
		{
			&plugin.Build{Ref: "refs/heads/master"},
			false,
			50,
			[]string{
				"git",
				"fetch",
				"--no-tags",
				"--depth=50",
				"origin",
				"+refs/heads/master:",
			},
		},
		{
			&plugin.Build{Ref: "refs/heads/master"},
			true,
			100,
			[]string{
				"git",
				"fetch",
				"--tags",
				"--depth=100",
				"origin",
				"+refs/heads/master:",
			},
		},
	}
	for _, td := range testdata {
		c := fetch(td.build, td.tags, td.depth, false)
		if len(c.Args) != len(td.exp) {
			t.Errorf("Expected: %s, got %s", td.exp, c.Args)
		}
		for i := range c.Args {
			if c.Args[i] != td.exp[i] {
				t.Errorf("Expected: %s, got %s", td.exp, c.Args)
			}
		}
	}
}

// TestUpdateSubmodules tests if the arguments to `git submodule update`
// are constructed properly.
func TestUpdateSubmodules(t *testing.T) {
	testdata := []struct {
		depth int
		exp   []string
	}{
		{
			50,
			[]string{
				"git",
				"submodule",
				"update",
				"--init",
				"--recursive",
			},
		},
		{
			100,
			[]string{
				"git",
				"submodule",
				"update",
				"--init",
				"--recursive",
			},
		},
	}
	for _, td := range testdata {
		c := updateSubmodules(false)
		if len(c.Args) != len(td.exp) {
			t.Errorf("Expected: %s, got %s", td.exp, c.Args)
		}
		for i := range c.Args {
			if c.Args[i] != td.exp[i] {
				t.Errorf("Expected: %s, got %s", td.exp, c.Args)
			}
		}
	}
}

// TestUpdateSubmodules tests if the arguments to `git submodule update`
// are constructed properly.
func TestUpdateSubmodulesRemote(t *testing.T) {
	testdata := []struct {
		depth int
		exp   []string
	}{
		{
			50,
			[]string{
				"git",
				"submodule",
				"update",
				"--init",
				"--recursive",
				"--remote",
			},
		},
		{
			100,
			[]string{
				"git",
				"submodule",
				"update",
				"--init",
				"--recursive",
				"--remote",
			},
		},
	}
	for _, td := range testdata {
		c := updateSubmodules(true)
		if len(c.Args) != len(td.exp) {
			t.Errorf("Expected: %s, got %s", td.exp, c.Args)
		}
		for i := range c.Args {
			if c.Args[i] != td.exp[i] {
				t.Errorf("Expected: %s, got %s", td.exp, c.Args)
			}
		}
	}
}

// helper function that will setup a temporary workspace.
// to which we can clone the repositroy
func setup() string {
	dir, _ := ioutil.TempDir("", "drone_git_test_")
	os.Mkdir(dir, 0777)
	return dir
}

// helper function to delete the temporary workspace.
func teardown(dir string) {
	if runtime.GOOS == "windows" {
		clearReadOnly(dir)
	}
	os.RemoveAll(dir)
}

// helper function to read a file in the temporary worskapce.
func readFile(dir, file string) string {
	filename := filepath.Join(dir, file)
	data, _ := ioutil.ReadFile(filename)
	return string(data)
}

// getTags returns all of the tags in a git repository as a map.
func getTags(dir string) (map[string]bool, error) {
	cmd := exec.Command("git", "tag")
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	tags := make(map[string]bool)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		tags[scanner.Text()] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	return tags, nil
}

// getSubmodules returns all of the submodules in a git repository as a map.
func getSubmodules(dir string) (map[string]string, error) {
	cmd := exec.Command("git", "submodule", "status")
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	submodules := make(map[string]string)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		a := strings.Split(strings.TrimSpace(scanner.Text()), " ")
		submodules[a[1]] = a[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	return submodules, nil
}

// Tries to remove READONLY mark from file (recursive)
// On Windows, os.Remove does not work if file was marked as READONLY (for example, git does it)
func clearReadOnly(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if !fi.IsDir() {
		return os.Chmod(path, 0666)
	}

	fd, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fd.Close()

	names, _ := fd.Readdirnames(-1)
	for _, name := range names {
		err = clearReadOnly(filepath.Join(path, name))
		if err != nil {
			return err
		}
	}
	return nil
}
