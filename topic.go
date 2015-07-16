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

// TODO(ericsnow) None of the topics methods are thread-safe...

type topics struct {
	order   []string
	topics  map[string]topic
	aliases map[string]string
}

func newTopics(initial ...topic) (topics, error) {
	t := topics{
		topics:  make(map[string]topic),
		aliases: make(map[string]string),
	}
	for _, topic := range initial {
		if err := t.addWithAliases(topic); err != nil {
			return t, err
		}
	}
	return t, nil
}

func (t *topics) names() []string {
	copied := make([]string, len(t.order))
	copy(copied, t.order)
	return copied
}

func (t *topics) namesWithoutAliases() []string {
	var names []string
	for _, name := range t.names() {
		if _, ok := t.aliases[name]; ok {
			continue
		}
		names = append(names, name)
	}
	return names
}

func (t *topics) lookUp(name string) (topic, bool) {
	topic, ok := t.topics[name]
	if ok {
		return topic, true
	}
	aliased, ok := t.aliases[name]
	if ok {
		return t.lookUp(aliased)
	}
	return topic, false
}

func (t *topics) add(topic topic) error {
	if _, found := t.lookUp(topic.name); found {
		return fmt.Errorf("help topic already added: %s", topic.name)
	}
	t.topics[topic.name] = topic
	t.order = append(t.order, topic.name)
	return nil
}

func (t *topics) addWithAliases(topic topic) error {
	if err := t.add(topic); err != nil {
		return err
	}

	var added []string
	for _, alias := range topic.aliases {
		if err := t.addAlias(topic.name, alias); err != nil {
			t.remove(topic.name)
			for _, name := range added {
				t.remove(name)
			}
			return err
		}
		added = append(added, alias)
	}

	return nil
}

func (t *topics) addAlias(name, alias string) error {
	// TODO(ericsnow) Allow aliasing other aliases?
	if _, ok := t.topics[name]; !ok {
		return fmt.Errorf("topic %q not found", name)
	}
	if _, ok := t.lookUp(alias); !ok {
		return fmt.Errorf("topic %q already added", alias)
	}
	t.aliases[alias] = name
	t.order = append(t.order, alias)
	return nil
}

func (t *topics) remove(name string) topic {
	topic, ok := t.topics[name]
	if !ok {
		if _, ok := t.aliases[name]; !ok {
			return topic
		}
		delete(t.aliases, name)
	} else {
		delete(t.topics, name)
	}
	t.order, _ = removeString(t.order, name)
	return topic
}

func (t *topics) removeWithAliases(name string) topic {
	topic := t.remove(name)
	if topic.name == "" {
		return topic
	}
	for _, alias := range topic.aliases {
		if _, ok := t.topics[alias]; ok {
			continue
		}
		t.remove(alias)
	}
	return topic
}

func removeString(items []string, target string) ([]string, bool) {
	last := len(items) - 1
	for i, item := range items {
		if item == target {
			if i == last {
				return items[:i], true
			}
			return append(items[:i], items[i+1:]...), true
		}
	}
	return items, false
}
