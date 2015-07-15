// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENSE file for details.

package cmd

import (
//"fmt"
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

func (copied topic) alias(name string) topic {
	copied.name = name
	copied.isAlias = true
	copied.aliases = nil
	return copied
}
