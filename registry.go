// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENSE file for details.

package cmd

import (
	"fmt"
)

// An Action is what a super-command performs for a given
// sub-command name.
type Action struct {
	// Name is the registered name of the action.
	Name string
	// AliasedName is the name of the action which this action aliases
	// (or nothing in the case that this is not an alias).
	AliasedName string
	// Command is the command which this action wraps.
	Command Command
	// Aliases is the action's alias names.
	Aliases []string
	// Replacement indicates then updated name to use in the case of
	// deprecated commands.
	Replacement string
}

func newActionFromCommand(cmd Command) Action {
	info := cmd.Info()
	action := Action{
		Name:    info.Name,
		Command: cmd,
		Aliases: info.Aliases,
	}
	return action
}

// Validate ensures that the action is valid.
func (a Action) Validate() error {
	if a.Name == "" {
		return fmt.Errorf("action missing name")
	}
	if a.Command == nil {
		return fmt.Errorf("action missing command")
	}
	return nil
}

// Summary returns a short description of the action.
func (a Action) Summary() string {
	info := a.Command.Info()
	purpose := info.Purpose
	if a.AliasedName != "" {
		purpose = "alias for '" + a.AliasedName + "'"
	}
	return purpose
}

// NewAlias creates a new Action with Name and AliasedName properly
// set. Command is kept the same.
func (a Action) NewAlias(alias string) Action {
	return Action{
		Name:        alias,
		AliasedName: a.Name,
		Command:     a.Command,
	}
}

// Info returns info about the action's command.
func (a Action) Info() *Info {
	return a.Command.Info()
}

// Init initialized the action's command.
func (a Action) Init(args []string) error {
	return a.Command.Init(args)
}

// PreRun performs action-specific tasks before the action is run.
func (a Action) PreRun(ctx *Context) error {
	if a.Replacement != "" {
		ctx.Infof("WARNING: %q is deprecated, please use %q", a.Name, a.Replacement)
	}
	return nil
}

// Run runs the action's command.
func (a Action) Run(ctx *Context) error {
	return a.Command.Run(ctx)
}

// TODO(ericsnow) Registry methods are not thread-safe...
// TODO(ericsnow) Merge help topics with commands (as actions)?
// TODO(ericsnow) Also support registering help topics?

// A Registry holds an insertion-order-preserving mapping of names
// to actions.
type Registry struct {
	order       []string
	actions     map[string]Action
	aliases     map[string]string
	defaultName string
}

// NewRegistry creates a new Registry and pre-populates it with the
// provided actions.
func NewRegistry(initial ...Action) (*Registry, error) {
	reg := &Registry{
		actions: make(map[string]Action),
		aliases: make(map[string]string),
	}
	for _, action := range initial {
		if err := reg.AddWithAliases(action); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// NewRegistryWithDefault creates a new Registry, adds the given
// action, and sets it as the default.
func NewRegistryWithDefault(dflt Action) (*Registry, error) {
	reg, err := NewRegistry(dflt)
	if err != nil {
		return nil, err
	}
	reg.defaultName = dflt.Name
	return reg, nil
}

// Names returns the insertion-ordered list of registered action names.
func (reg *Registry) Names() []string {
	copied := make([]string, len(reg.order))
	copy(copied, reg.order)
	return copied
}

// LookUp finds the named action in the registry. If more than one name
// is provided then the lookup traverses the named actions on the
// effective path. If any of the parent actions does not wrap a
// super-command then the lookup fails. If the identified action is not
// found, for whatever reason, then false is returned.
func (reg *Registry) LookUp(path ...string) (Action, bool) {
	if len(path) == 0 {
		return reg.lookUp(reg.defaultName)
	}

	var action Action
	for _, name := range path {
		if reg == nil {
			break
		}
		next, ok := reg.lookUp(name)
		if !ok {
			break
		}
		action = next
		if super, ok := action.Command.(*SuperCommand); ok {
			reg = super.subcmds
		} else {
			break
		}
	}

	if action.Name != "" {
		return action, true
	}
	return action, false
}

func (reg *Registry) lookUp(name string) (Action, bool) {
	action, ok := reg.actions[name]
	if ok {
		return action, true
	}
	if aliased, ok := reg.aliases[name]; ok {
		action, ok := reg.actions[aliased]
		if ok {
			return action.NewAlias(name), true
		}
	}
	return action, false
}

// Add adds the action to the registry. If it is already registered
// then the request fails.
func (reg *Registry) Add(action Action) error {
	if err := action.Validate(); err != nil {
		return err
	}

	if _, found := reg.LookUp(action.Name); found {
		return fmt.Errorf("command %q already registered", action.Name)
	}
	reg.actions[action.Name] = action
	reg.order = append(reg.order, action.Name)
	return nil
}

// AddWithAliases adds the given action to the registry in the same way
// as Add. However, it also adds an alias for each of the action's
// aliases.
func (reg *Registry) AddWithAliases(action Action) error {
	if err := reg.Add(action); err != nil {
		return err
	}

	var added []string
	for _, alias := range action.Aliases {
		if err := reg.AddAlias(action.Name, alias); err != nil {
			reg.remove(action.Name)
			for _, alias := range added {
				reg.remove(alias)
			}
			return err
		}
		added = append(added, alias)
	}

	return nil
}

// AddAlias adds an alias between the two given names.
func (reg *Registry) AddAlias(name, alias string) error {
	if _, ok := reg.actions[name]; !ok {
		return fmt.Errorf("action %q not found", name)
	}
	if _, ok := reg.LookUp(alias); ok {
		return fmt.Errorf("action %q already added", alias)
	}
	reg.aliases[alias] = name
	reg.order = append(reg.order, alias)
	return nil
}

func (reg *Registry) remove(name string) Action {
	action, ok := reg.actions[name]
	if !ok {
		if _, ok := reg.aliases[name]; !ok {
			return action
		}
		delete(reg.aliases, name)
	} else {
		delete(reg.actions, name)
	}
	reg.order, _ = removeString(reg.order, name)
	return action
}
