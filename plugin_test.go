// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package cmd_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"text/template"
	"time"

	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/cmd"
	"github.com/juju/cmd/cmdtesting"
)

type pluginSuite struct {
	testing.IsolationSuite
	rootDir string
	oldPath string
	plugins *cmd.Plugins
}

var _ = gc.Suite(&pluginSuite{})

func (suite *pluginSuite) SetUpTest(c *gc.C) {
	//TODO(bogdanteleaga): Fix bash tests
	if runtime.GOOS == "windows" {
		c.Skip("bug 1403084: tests use bash scrips, will be rewritten for windows")
	}
	suite.IsolationSuite.SetUpTest(c)

	suite.rootDir = c.MkDir()
	suite.oldPath = os.Getenv("PATH")
	os.Setenv("PATH", "/bin:"+suite.rootDir)

	suite.plugins = cmd.NewPlugins("juju-", "Juju Plugins")
	suite.plugins.IgnoredFlags = []string{"-e"}
}

func (suite *pluginSuite) TearDownTest(c *gc.C) {
	os.Setenv("PATH", suite.oldPath)

	suite.IsolationSuite.TearDownTest(c)
}

func (suite *pluginSuite) TestFindAllOrder(c *gc.C) {
	suite.makePlugin("foo", 0744)
	suite.makePlugin("bar", 0654)
	suite.makePlugin("baz", 0645)

	os.Setenv("PATH", suite.oldPath)
	path := []string{"/bin", suite.rootDir}
	plugins := suite.plugins.FindAll(path)

	c.Check(plugins, jc.DeepEquals, []string{"juju-bar", "juju-baz", "juju-foo"})
}

func (suite *pluginSuite) TestFindAllEmpty(c *gc.C) {
	plugins := suite.plugins.FindAll(nil)

	c.Check(plugins, jc.DeepEquals, []string{})
}

func (suite *pluginSuite) TestFindAllIgnoreNotExec(c *gc.C) {
	suite.makePlugin("foo", 0644)
	suite.makePlugin("bar", 0666)

	plugins := suite.plugins.FindAll(nil)

	c.Check(plugins, jc.DeepEquals, []string{})
}

func (suite *pluginSuite) TestRunPluginExising(c *gc.C) {
	suite.makePlugin("foo", 0755)

	ctx := cmdtesting.Context(c)
	err := suite.plugins.RunPlugin(ctx, "foo", []string{"some params"})
	c.Assert(err, jc.ErrorIsNil)

	c.Check(cmdtesting.Stdout(ctx), gc.Equals, "foo some params\n")
	c.Check(cmdtesting.Stderr(ctx), gc.Equals, "")
}

func (suite *pluginSuite) TestRunPluginWithFailing(c *gc.C) {
	suite.makeFailingPlugin("foo", 2)

	ctx := cmdtesting.Context(c)
	err := suite.plugins.RunPlugin(ctx, "foo", []string{"some params"})

	c.Check(err, gc.ErrorMatches, "subprocess encountered error code 2")
	c.Check(err, jc.Satisfies, cmd.IsRcPassthroughError)
	c.Check(cmdtesting.Stdout(ctx), gc.Equals, "failing\n")
	c.Check(cmdtesting.Stderr(ctx), gc.Equals, "")
}

func (suite *pluginSuite) TestGatherDescriptionsInParallel(c *gc.C) {
	// Make plugins that will deadlock if we don't start them in parallel.
	// Each plugin depends on another one being started before they will
	// complete. They make a full loop, so no sequential ordering will ever
	// succeed.
	suite.makeFullPlugin(PluginParams{Name: "foo", Creates: "foo", DependsOn: "bar"})
	suite.makeFullPlugin(PluginParams{Name: "bar", Creates: "bar", DependsOn: "baz"})
	suite.makeFullPlugin(PluginParams{Name: "baz", Creates: "baz", DependsOn: "error"})
	suite.makeFullPlugin(PluginParams{Name: "error", ExitStatus: 1, Creates: "error", DependsOn: "foo"})

	// If the code was wrong, GetPluginDescriptions would deadlock,
	// so timeout after a short while
	resultChan := make(chan map[string]string)
	go func() {
		resultChan <- suite.plugins.Descriptions()
	}()
	// 10 seconds is arbitrary but should always be generously long. Test
	// actually only takes about 15ms in practice. But 10s allows for system hiccups, etc.
	waitTime := 10 * time.Second
	var results map[string]string
	select {
	case results = <-resultChan:
		break
	case <-time.After(waitTime):
		c.Fatalf("took longer than %fs to complete.", waitTime.Seconds())
	}

	var names []string
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	var descriptions []string
	for _, name := range names {
		descriptions = append(descriptions, results[name])
	}
	c.Check(names, jc.DeepEquals, []string{
		"bar",
		"baz",
		"error",
		"foo",
	})
	c.Check(descriptions, jc.DeepEquals, []string{
		"bar description",
		"baz description",
		"error occurred running 'juju-error --description'",
		"foo description",
	})
}

func (suite *pluginSuite) TestHelpPluginsWithNoPlugins(c *gc.C) {
	output := suite.plugins.HelpTopic()

	c.Check(output, gc.Equals, `Juju Plugins

Plugins are implemented as stand-alone executable files somewhere
in the user's PATH. The executable command must be of the format
"juju-<plugin name>".

No plugins found.
`)
}

func (suite *pluginSuite) TestHelpPluginsWithPlugins(c *gc.C) {
	suite.makeFullPlugin(PluginParams{Name: "foo"})
	suite.makeFullPlugin(PluginParams{Name: "bar"})

	output := suite.plugins.HelpTopic()

	c.Check(output, gc.Equals, `Juju Plugins

Plugins are implemented as stand-alone executable files somewhere
in the user's PATH. The executable command must be of the format
"juju-<plugin name>".

bar  bar description
foo  foo description
`)
}

func (suite *pluginSuite) resolve(name string) string {
	return filepath.Join(suite.rootDir, name)
}

func (suite *pluginSuite) makePlugin(name string, perm os.FileMode) {
	content := fmt.Sprintf("#!/bin/bash --norc\necho %s $*", name)
	filename := suite.resolve(suite.plugins.Prefix + name)
	ioutil.WriteFile(filename, []byte(content), perm)
}

func (suite *pluginSuite) makeFailingPlugin(name string, exitStatus int) {
	content := fmt.Sprintf("#!/bin/bash --norc\necho failing\nexit %d", exitStatus)
	filename := suite.resolve(suite.plugins.Prefix + name)
	ioutil.WriteFile(filename, []byte(content), 0755)
}

type PluginParams struct {
	Name       string
	ExitStatus int
	Creates    string
	DependsOn  string
}

const pluginTemplate = `#!/bin/bash --norc

if [ "$1" = "--description" ]; then
  if [ -n "{{.Creates}}" ]; then
    touch "{{.Creates}}"
  fi
  if [ -n "{{.DependsOn}}" ]; then
    # Sleep 10ms while waiting to allow other stuff to do work
    while [ ! -e "{{.DependsOn}}" ]; do sleep 0.010; done
  fi
  echo "{{.Name}} description"
  exit {{.ExitStatus}}
fi

if [ "$1" = "--help" ]; then
  echo "{{.Name}} longer help"
  echo ""
  echo "something useful"
  exit {{.ExitStatus}}
fi

if [ "$1" = "--debug" ]; then
  echo "some debug"
  exit {{.ExitStatus}}
fi

echo {{.Name}} $*
echo "env is: " $JUJU_ENV
echo "home is: " $JUJU_HOME
exit {{.ExitStatus}}
`

func (suite *pluginSuite) makeFullPlugin(params PluginParams) {
	// Create a new template and parse the plugin into it.
	t := template.Must(template.New("plugin").Parse(pluginTemplate))
	content := &bytes.Buffer{}
	filename := suite.resolve("juju-" + params.Name)
	// Create the files in the temp dirs, so we don't pollute the working space
	if params.Creates != "" {
		params.Creates = suite.resolve(params.Creates)
	}
	if params.DependsOn != "" {
		params.DependsOn = suite.resolve(params.DependsOn)
	}
	t.Execute(content, params)
	ioutil.WriteFile(filename, content.Bytes(), 0755)
}
