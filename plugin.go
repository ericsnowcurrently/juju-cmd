// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"launchpad.net/gnuflag"
)

// Plugins represents the plugins supported by a super-command.
type Plugins struct {
	// The executable filename prefix for plugins.
	Prefix string
	// IgnoredFlags is the list of non-bool flags that will be ignored.
	IgnoredFlags []string
	// Env is the extra OS environment variables to use for the plugin,
	// in the format of "<name>=<value>".
	Env []string
	// Title is the title to use for the plugins help topic
	// on the super-command.
	Title string
}

// RunPlugin may be used for SuperCommandParams.MissingCallback.
func (p Plugins) RunPlugin(ctx *Context, subcommand string, args []string) error {
	cmdName := p.Prefix + subcommand
	plugin := &PluginCommand{
		name: cmdName,
		env:  p.Env,
	}

	// We process common flags supported by the super-command.
	// To do this, we extract only those supported flags from the
	// argument list to avoid confusing flags.Parse().
	flags := gnuflag.NewFlagSet(cmdName, gnuflag.ContinueOnError)
	flags.SetOutput(ioutil.Discard)
	plugin.SetFlags(flags)
	ignoredArgs := p.extractIgnoredArgs(args)
	if err := flags.Parse(false, ignoredArgs); err != nil {
		return err
	}

	// Now init and run the command.
	if err := plugin.Init(args); err != nil {
		return err
	}
	err := plugin.Run(ctx)
	if _, ok := err.(*exec.Error); ok {
		// exec.Error results are for when the executable isn't found.
		return &UnrecognizedCommand{Name: subcommand}
	}
	return err
}

// extractIgnoredArgs is a very rudimentary method used to extract
// common super-command arguments from the full list passed to the
// plugin.
func (p Plugins) extractIgnoredArgs(args []string) []string {
	var ignoredArgs []string
	nrArgs := len(args)
	for nextArg := 0; nextArg < nrArgs; {
		arg := args[nextArg]
		nextArg++
		for _, recognized := range p.IgnoredFlags {
			if arg == recognized {
				ignoredArgs = append(ignoredArgs, arg)
				if nextArg < nrArgs {
					ignoredArgs = append(ignoredArgs, args[nextArg])
					nextArg++
				}
				break
			}
		}
	}
	return ignoredArgs
}

const pluginTopicText = `%s

Plugins are implemented as stand-alone executable files somewhere
in the user's PATH. The executable command must be of the format
"%s<plugin name>".

`

// HelpTopic returns the help topic string for a supercommand
// that supports plugins.
func (p Plugins) HelpTopic() string {
	output := &bytes.Buffer{}
	fmt.Fprintf(output, pluginTopicText, p.Title, p.Prefix)

	existingPlugins := p.Descriptions()

	if len(existingPlugins) == 0 {
		fmt.Fprintf(output, "No plugins found.\n")
	} else {
		longest := 0
		var names []string
		for name := range existingPlugins {
			names = append(names, name)
			if len(name) > longest {
				longest = len(name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			description := existingPlugins[name]
			fmt.Fprintf(output, "%-*s  %s\n", longest, name, description)
		}
	}

	return output.String()
}

type pluginDescription struct {
	name        string
	description string
}

// Descriptions runs each plugin with "--description".  The calls to
// the plugins are run in parallel, so the function should only take as long
// as the longest call.
func (p Plugins) Descriptions() map[string]string {
	plugins := p.FindAll(nil)
	if len(plugins) == 0 {
		return nil
	}

	// create a channel with enough backing for each plugin
	description := make(chan pluginDescription, len(plugins))

	// exec the command, and wait only for the timeout before killing the process
	for _, plugin := range plugins {
		go func(plugin string) {
			result := pluginDescription{name: plugin}
			defer func() {
				description <- result
			}()
			desccmd := exec.Command(plugin, "--description")
			output, err := desccmd.CombinedOutput()

			if err == nil {
				// trim to only get the first line
				result.description = strings.SplitN(string(output), "\n", 2)[0]
			} else {
				result.description = fmt.Sprintf("error occurred running '%s --description'", plugin)
				logger.Errorf("'%s --description': %s", plugin, err)
			}
		}(plugin)
	}

	// Gather the results at the end.
	results := make(map[string]string, len(plugins))
	for _ = range plugins {
		result := <-description
		name := result.name[len(p.Prefix):]
		results[name] = result.description
	}
	return results
}

// FindAll searches the current PATH for executable files that start with
// the given prefix.
func (p Plugins) FindAll(path []string) []string {
	if path == nil {
		path = filepath.SplitList(os.Getenv("PATH"))
	}
	var plugins []string
	for _, name := range path {
		entries, err := ioutil.ReadDir(name)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), p.Prefix) && (entry.Mode()&0111) != 0 {
				plugins = append(plugins, entry.Name())
			}
		}
	}
	sort.Strings(plugins)
	return plugins
}

// PluginCommand is a Command that wraps a plugin for a super-command.
type PluginCommand struct {
	CommandBase
	name string
	args []string
	env  []string
}

// Info is just a stub so that PluginCommand implements Command.
// Since this is never actually called, we can happily return nil.
func (*PluginCommand) Info() *Info {
	return nil
}

// Init implements Command.
func (c *PluginCommand) Init(args []string) error {
	c.args = args
	return nil
}

// Run implements Command.
func (c *PluginCommand) Run(ctx *Context) error {
	command := exec.Command(c.name, c.args...)
	command.Env = append(os.Environ(), c.env...)

	// Now hook up stdin, stdout, stderr
	command.Stdin = ctx.Stdin
	command.Stdout = ctx.Stdout
	command.Stderr = ctx.Stderr
	// And run it!
	err := command.Run()

	if exitError, ok := err.(*exec.ExitError); ok && exitError != nil {
		status := exitError.ProcessState.Sys().(syscall.WaitStatus)
		if status.Exited() {
			return NewRcPassthroughError(status.ExitStatus())
		}
	}
	return err
}
