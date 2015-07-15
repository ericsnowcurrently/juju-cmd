// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENSE file for details.

package cmd

import (
	"fmt"
)

// UnrecognizedCommand is an error indicating that the named command is
// not recognized.
type UnrecognizedCommand struct {
	// Name is the name of the command.
	Name string
}

// Error implements error.
func (e *UnrecognizedCommand) Error() string {
	return fmt.Sprintf("unrecognized command: %s", e.Name)
}

// MissingCallback defines a function that will be used by the SuperCommand if
// the requested subcommand isn't found.
type MissingCallback func(ctx *Context, subcommand string, args []string) error

// missingCommand is used when a named sub-command is not found.
type missingCommand struct {
	CommandBase
	// Callback is the handler used for the "command" when it is run.
	Callback MissingCallback
	// SuperName is the name of the super-command on which the command
	// was not found.
	SuperName string
	// Name is the name of the sub-command that wasn't found
	Name string
	// Args is the args with which the sub-command would have
	// been called.
	Args []string
}

// Info implements Command. Missing commands only need to supply it for
// the interface, but this is never called.
func (c *missingCommand) Info() *Info {
	return nil
}

// Run implements Command. It is called directly by the super-command
// when a requested sub-command is not found.
func (c *missingCommand) Run(ctx *Context) error {
	err := c.Callback(ctx, c.Name, c.Args)
	if _, isUnrecognized := err.(*UnrecognizedCommand); !isUnrecognized {
		return err
	}
	return &UnrecognizedCommand{c.SuperName + " " + c.Name}
}
