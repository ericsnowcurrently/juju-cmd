// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the LGPLv3, see LICENSE file for details.

package cmd

import (
	"fmt"
	"io/ioutil"
	"sort"
	"strings"

	"github.com/juju/loggo"
	"launchpad.net/gnuflag"
)

var logger = loggo.GetLogger("juju.cmd")

// DeprecationCheck is used to provide callbacks to determine if
// a command is deprecated or obsolete.
type DeprecationCheck interface {

	// Deprecated aliases emit a warning when executed. If the command is
	// deprecated, the second return value recommends what to use instead.
	Deprecated() (bool, string)

	// Obsolete aliases are not actually registered. The purpose of this
	// is to allow code to indicate ahead of time some way to determine
	// that the command should stop working.
	Obsolete() bool
}

// SuperCommandParams provides a way to have default parameter to the
// `NewSuperCommand` call.
type SuperCommandParams struct {
	// UsagePrefix should be set when the SuperCommand is
	// actually a subcommand of some other SuperCommand;
	// if NotifyRun is called, it name will be prefixed accordingly,
	// unless UsagePrefix is identical to Name.
	UsagePrefix string

	// Notify, if not nil, is called when the SuperCommand
	// is about to run a sub-command.
	NotifyRun func(cmdName string)

	Name            string
	Purpose         string
	Doc             string
	Log             *Log
	MissingCallback MissingCallback
	Aliases         []string
	Version         string
}

// NewSuperCommand creates and initializes a new `SuperCommand`, and returns
// the fully initialized structure.
func NewSuperCommand(params SuperCommandParams) *SuperCommand {
	command := &SuperCommand{
		Name:            params.Name,
		Purpose:         params.Purpose,
		Doc:             params.Doc,
		Log:             params.Log,
		usagePrefix:     params.UsagePrefix,
		missingCallback: params.MissingCallback,
		Aliases:         params.Aliases,
		version:         params.Version,
		notifyRun:       params.NotifyRun,
	}
	command.init()
	return command
}

// SuperCommand is a Command that selects a subcommand and assumes its
// properties; any command line arguments that were not used in selecting
// the subcommand are passed down to it, and to Run a SuperCommand is to run
// its selected subcommand.
type SuperCommand struct {
	CommandBase
	Name            string
	Purpose         string
	Doc             string
	Log             *Log
	Aliases         []string
	version         string
	usagePrefix     string
	subcmds         *Registry
	help            *helpCommand
	commonflags     *gnuflag.FlagSet
	flags           *gnuflag.FlagSet
	action          Action
	showHelp        bool
	showDescription bool
	showVersion     bool
	missingCallback MissingCallback
	notifyRun       func(string)
}

// IsSuperCommand implements Command.IsSuperCommand
func (c *SuperCommand) IsSuperCommand() bool {
	return true
}

func (c *SuperCommand) init() {
	if c.subcmds != nil {
		return
	}
	c.help = &helpCommand{
		super: c,
	}
	c.help.init()
	c.subcmds, _ = NewRegistryWithDefault(Action{
		Name:    "help",
		Command: c.help,
	})
	if c.version != "" {
		c.subcmds.Add(Action{
			Name:    "version",
			Command: newVersionCommand(c.version),
		})
	}
}

// AddHelpTopic adds a new help topic with the description being the short
// param, and the full text being the long param.  The description is shown in
// 'help topics', and the full text is shown when the command 'help <name>' is
// called.
func (c *SuperCommand) AddHelpTopic(name, short, long string, aliases ...string) {
	c.help.addTopic(name, short, echo(long), aliases...)
}

func echo(s string) func() string {
	return func() string { return s }
}

// AddHelpTopicCallback adds a new help topic with the description being the
// short param, and the full text being defined by the callback function.
func (c *SuperCommand) AddHelpTopicCallback(name, short string, longCallback func() string) {
	c.help.addTopic(name, short, longCallback)
}

// Register makes a subcommand available for use on the command line. The
// command will be available via its own name, and via any supplied aliases.
func (c *SuperCommand) Register(subcmd Command) {
	if subcmd == nil {
		return
	}
	action := newActionFromCommand(subcmd)
	if err := c.subcmds.AddWithAliases(action); err != nil {
		panic(err.Error())
	}
}

// RegisterDeprecated makes a subcommand available for use on the command line if it
// is not obsolete.  It inserts the command with the specified DeprecationCheck so
// that a warning is displayed if the command is deprecated.
func (c *SuperCommand) RegisterDeprecated(subcmd Command, check DeprecationCheck) {
	if subcmd == nil {
		return
	}

	action := newActionFromCommand(subcmd)
	if check != nil && check.Obsolete() {
		logger.Infof("%q command not registered as it is obsolete", action.Name)
		return
	}
	if deprecated, replacement := check.Deprecated(); deprecated {
		action.Replacement = replacement
	}

	if err := c.subcmds.AddWithAliases(action); err != nil {
		panic(err.Error())
	}
}

// RegisterAlias makes an existing subcommand available under another name.
// If `check` is supplied, and the result of the `Obsolete` call is true,
// then the alias is not registered.
func (c *SuperCommand) RegisterAlias(name, forName string, check DeprecationCheck) {
	if check != nil && check.Obsolete() {
		logger.Infof("%q alias not registered as it is obsolete", name)
		return
	}

	if err := c.subcmds.AddAlias(forName, name); err != nil {
		panic(err.Error())
	}
}

// RegisterSuperAlias makes a subcommand of a registered supercommand
// available under another name. This is useful when the command structure is
// being refactored.  If `check` is supplied, and the result of the `Obsolete`
// call is true, then the alias is not registered.
func (c *SuperCommand) RegisterSuperAlias(name, super, forName string, check DeprecationCheck) {
	if check != nil && check.Obsolete() {
		logger.Infof("%q alias not registered as it is obsolete", name)
		return
	}

	aliased := super + " " + forName
	action, ok := c.subcmds.LookUp(super, forName)
	if !ok {
		if !action.Command.IsSuperCommand() {
			panic(fmt.Sprintf("%q is not a SuperCommand", super))
		}
		panic(fmt.Sprintf("%q not found when registering alias", aliased))
	}

	action = action.NewAlias(name)
	action.AliasedName = aliased
	if err := c.subcmds.Add(action); err != nil {
		panic(err.Error())
	}
}

// describeCommands returns a short description of each registered subcommand.
func (c *SuperCommand) describeCommands(simple bool) string {
	var lineFormat = "    %-*s - %s"
	var outputFormat = "commands:\n%s"
	if simple {
		lineFormat = "%-*s  %s"
		outputFormat = "%s"
	}
	cmds := c.subcmds.Names()
	sort.Strings(cmds)

	longest := 0
	for _, name := range cmds {
		if len(name) > longest {
			longest = len(name)
		}
	}
	var result []string
	for _, name := range cmds {
		action, _ := c.subcmds.LookUp(name)
		if action.Replacement != "" {
			continue
		}
		purpose := action.Summary()
		result = append(result, fmt.Sprintf(lineFormat, longest, name, purpose))
	}
	return fmt.Sprintf(outputFormat, strings.Join(result, "\n"))
}

// Info returns a description of the currently selected subcommand, or of the
// SuperCommand itself if no subcommand has been specified.
func (c *SuperCommand) Info() *Info {
	if err := c.action.Validate(); err == nil {
		info := *c.action.Info()
		info.Name = fmt.Sprintf("%s %s", c.Name, info.Name)
		return &info
	}
	docParts := []string{}
	if doc := strings.TrimSpace(c.Doc); doc != "" {
		docParts = append(docParts, doc)
	}
	if cmds := c.describeCommands(false); cmds != "" {
		docParts = append(docParts, cmds)
	}
	return &Info{
		Name:    c.Name,
		Args:    "<command> ...",
		Purpose: c.Purpose,
		Doc:     strings.Join(docParts, "\n\n"),
		Aliases: c.Aliases,
	}
}

const helpPurpose = "show help on a command or other topic"

// SetCommonFlags creates a new "commonflags" flagset, whose
// flags are shared with the argument f; this enables us to
// add non-global flags to f, which do not carry into subcommands.
func (c *SuperCommand) SetCommonFlags(f *gnuflag.FlagSet) {
	if c.Log != nil {
		c.Log.AddFlags(f)
	}
	f.BoolVar(&c.showHelp, "h", false, helpPurpose)
	f.BoolVar(&c.showHelp, "help", false, "")
	// In the case where we are providing the basis for a plugin,
	// plugins are required to support the --description argument.
	// The Purpose attribute will be printed (if defined), allowing
	// plugins to provide a sensible line of text for 'juju help plugins'.
	f.BoolVar(&c.showDescription, "description", false, "")
	c.commonflags = gnuflag.NewFlagSet(c.Info().Name, gnuflag.ContinueOnError)
	c.commonflags.SetOutput(ioutil.Discard)
	f.VisitAll(func(flag *gnuflag.Flag) {
		c.commonflags.Var(flag.Value, flag.Name, flag.Usage)
	})
}

// SetFlags adds the options that apply to all commands, particularly those
// due to logging.
func (c *SuperCommand) SetFlags(f *gnuflag.FlagSet) {
	c.SetCommonFlags(f)
	// Only flags set by SetCommonFlags are passed on to subcommands.
	// Any flags added below only take effect when no subcommand is
	// specified (e.g. command --version).
	if c.version != "" {
		f.BoolVar(&c.showVersion, "version", false, "Show the command's version and exit")
	}
	c.flags = f
}

// For a SuperCommand, we want to parse the args with
// allowIntersperse=false. This will mean that the args may contain other
// options that haven't been defined yet, and that only options that relate
// to the SuperCommand itself can come prior to the subcommand name.
func (c *SuperCommand) AllowInterspersedFlags() bool {
	return false
}

// Init initializes the command for running.
func (c *SuperCommand) Init(args []string) error {
	if c.showDescription {
		return CheckEmpty(args)
	}
	if len(args) == 0 {
		c.action, _ = c.subcmds.LookUp()
		return c.action.Init(nil)
	}

	// Look up the action.
	name, args := args[0], args[1:]
	action, found := c.subcmds.LookUp(name)
	if !found {
		if c.missingCallback == nil {
			return fmt.Errorf("unrecognized command: %s %s", c.Name, name)
		}

		c.action = newActionFromCommand(&missingCommand{
			Callback:  c.missingCallback,
			SuperName: c.Name,
			Name:      name,
			Args:      args,
		})
		// Yes return here, no Init called on missing Command.
		return nil
	}
	c.action = action
	subcmd := c.action.Command

	// Handle sub-command options.
	if subcmd.IsSuperCommand() {
		f := gnuflag.NewFlagSet(c.Info().Name, gnuflag.ContinueOnError)
		f.SetOutput(ioutil.Discard)
		subcmd.SetFlags(f)
	} else {
		subcmd.SetFlags(c.commonflags)
	}
	if err := c.commonflags.Parse(subcmd.AllowInterspersedFlags(), args); err != nil {
		return err
	}
	args = c.commonflags.Args()

	// Handle the -h/--help options.
	if c.showHelp {
		// We want to treat help for the command the same way we would if we went "help foo".
		args = []string{c.action.Name}
		c.action, _ = c.subcmds.LookUp("help")
	}

	return c.action.Init(args)
}

// Run executes the subcommand that was selected in Init.
func (c *SuperCommand) Run(ctx *Context) error {
	if c.showDescription {
		if c.Purpose != "" {
			fmt.Fprintf(ctx.Stdout, "%s\n", c.Purpose)
		} else {
			fmt.Fprintf(ctx.Stdout, "%s: no description available\n", c.Info().Name)
		}
		return nil
	}

	// Run the action.
	if err := c.action.Validate(); err != nil {
		panic("Run: missing subcommand; Init failed or not called")
	}
	if err := c.preRun(ctx); err != nil {
		return err
	}
	if err := c.run(ctx); err != nil {
		return err
	}
	return nil
}

func (c *SuperCommand) preRun(ctx *Context) error {
	if c.Log != nil {
		if err := c.Log.Start(ctx); err != nil {
			return err
		}
	}
	if c.notifyRun != nil {
		name := c.Name
		if c.usagePrefix != "" && c.usagePrefix != name {
			name = c.usagePrefix + " " + name
		}
		c.notifyRun(name)
	}
	if err := c.action.PreRun(ctx); err != nil {
		return err
	}
	return nil
}

func (c *SuperCommand) run(ctx *Context) error {
	err := c.action.Run(ctx)
	if err != nil && err != ErrSilent {
		logger.Errorf("%v", err)
		// Now that this has been logged, don't log again in cmd.Main.
		if !IsRcPassthroughError(err) {
			err = ErrSilent
		}
	} else {
		logger.Infof("command finished")
	}
	return err
}
