// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENSE file for details.

package cmd

import (
	"fmt"
)

type topic struct {
	name  string
	short string
	long  func() string
	// Help aliases are not output when topics are listed, but are used
	// to search for the help topic
	isAlias bool
	aliases []string
}

func newTopic(name, short string, long func() string, aliases ...string) topic {
	return topic{
		name:    name,
		short:   short,
		long:    long,
		aliases: aliases,
	}
}

func (copied topic) newAlias(name string) topic {
	copied.name = name
	copied.isAlias = true
	copied.aliases = nil
	return copied
}

type topics map[string]topic

func (t topics) add(topic topic) error {
	if _, found := t[topic.name]; found {
		return fmt.Errorf("help topic already added: %s", topic.name)
	}
	t[topic.name] = topic
	return nil
}

func (t topics) addWithAliases(topic topic) error {
	var added []string

	if err := t.add(topic); err != nil {
		return err
	}
	added = append(added, topic.name)

	for _, alias := range topic.aliases {
		if err := t.add(topic.newAlias(alias)); err != nil {
			for _, name := range added {
				delete(t, name)
			}
			return err
		}
		added = append(added, alias)
	}

	return nil
}

func (t topics) addAlias(name, alias string) error {
	topic, ok := t[name]
	if !ok {
		return fmt.Errorf("topic %q not found", name)
	}
	return t.add(topic.newAlias(alias))
}
