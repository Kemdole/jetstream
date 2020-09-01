// Copyright 2020 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/AlecAivazis/survey/v2"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/nats-io/jetstream/nats/natscontext"
)

type ctxCommand struct {
	json        bool
	activate    bool
	description string
	name        string
	force       bool
}

func configureCtxCommand(app *kingpin.Application) {
	c := ctxCommand{}

	context := app.Command("context", `Manage nats configuration contexts

Context names can not contain your OS path separator or ".." but are otherwise
at your discretion.  There is one selected context which is recorded as
default, and the NATS_CONTEXT environment variable can override that.`).Alias("ctx")

	edit := context.Command("edit", "Edit a context in your EDITOR").Alias("vi").Action(c.editCommand)
	edit.Arg("name", "The context name to edit").Required().StringVar(&c.name)

	context.Command("ls", "List known contexts").Alias("list").Alias("l").Action(c.listCommand)

	rm := context.Command("rm", "Remove a context").Alias("remove").Action(c.removeCommand)
	rm.Arg("name", "The context name to remove").Required().StringVar(&c.name)
	rm.Flag("force", "Force remove without prompting").Short('f').BoolVar(&c.force)

	save := context.Command("save", "Update or create a context").Alias("add").Alias("create").Action(c.createCommand)
	save.Arg("name", "The context name to act on").Required().StringVar(&c.name)
	save.Flag("description", "Set a friendly description for this context").StringVar(&c.description)
	save.Flag("select", "Select the saved context as the default one").BoolVar(&c.activate)

	pick := context.Command("select", "Select the default context").Alias("switch").Alias("set").Action(c.selectCommand)
	pick.Arg("name", "The context name to select").StringVar(&c.name)

	show := context.Command("show", "Show the current or named context").Action(c.showCommand)
	show.Arg("name", "The context name to show").StringVar(&c.name)
	show.Flag("json", "Show the context in JSON format").Short('j').BoolVar(&c.json)
}

func (c *ctxCommand) editCommand(_ *kingpin.ParseContext) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return fmt.Errorf("set EDITOR environment variable to your chosen editor")
	}

	if !natscontext.IsKnown(c.name) {
		return fmt.Errorf("unknown context %q", c.name)
	}

	path, err := natscontext.ContextPath(c.name)
	if err != nil {
		return err
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (c *ctxCommand) listCommand(_ *kingpin.ParseContext) error {
	known := natscontext.KnownContexts()
	current := natscontext.SelectedContext()
	if len(known) == 0 {
		fmt.Println("No known contexts")
		return nil
	}

	fmt.Printf("Known contexts:\n\n")
	for _, name := range known {
		cfg, _ := natscontext.New(name, true)

		if name == current {
			name = name + "*"
		}

		if cfg != nil && cfg.Description() != "" {
			fmt.Printf("   %-20s%s\n", name, cfg.Description())
		} else {
			fmt.Printf("   %-20s\n", name)
		}
	}

	fmt.Println()

	return nil
}

func (c *ctxCommand) showCommand(_ *kingpin.ParseContext) error {
	if c.name == "" {
		c.name = natscontext.SelectedContext()
	}

	if c.name == "" {
		return fmt.Errorf("no default context and no name supplied")
	}

	cfg, err := natscontext.New(c.name, true)
	if err != nil {
		return err
	}

	if c.json {
		printJSON(cfg)
		return nil
	}

	fmt.Printf("NATS Configuration Context %q\n\n", c.name)
	c.showIfNotEmpty("  Description: %s\n", cfg.Description())
	c.showIfNotEmpty("  Server URLs: %s\n", cfg.ServerURL())
	c.showIfNotEmpty("     Username: %s\n", cfg.User())
	c.showIfNotEmpty("     Password: %s\n", cfg.Password())
	c.showIfNotEmpty("  Credentials: %s\n", cfg.Creds())
	c.showIfNotEmpty("         NKey: %s\n", cfg.NKey())
	c.showIfNotEmpty("  Certificate: %s\n", cfg.Certificate())
	c.showIfNotEmpty("          Key: %s\n", cfg.Key())
	c.showIfNotEmpty("           CA: %s\n", cfg.CA())
	c.showIfNotEmpty("         Path: %s\n", cfg.Path())
	fmt.Println()

	return nil
}
func (c *ctxCommand) createCommand(pc *kingpin.ParseContext) error {
	lname := ""
	load := false

	switch {
	case natscontext.IsKnown(c.name):
		lname = c.name
		load = true
	case cfgCtx != "":
		lname = cfgCtx
		load = true
	}

	config, err := natscontext.New(lname, load,
		natscontext.WithServerURL(servers),
		natscontext.WithUser(username),
		natscontext.WithPassword(password),
		natscontext.WithCreds(creds),
		natscontext.WithNKey(nkey),
		natscontext.WithCertificate(tlsCert),
		natscontext.WithKey(tlsKey),
		natscontext.WithCA(tlsCA),
		natscontext.WithDescription(c.description),
	)
	if err != nil {
		return err
	}

	err = config.Save(c.name)
	if err != nil {
		return err
	}

	if c.activate {
		return c.selectCommand(pc)
	}

	return nil
}

func (c *ctxCommand) removeCommand(_ *kingpin.ParseContext) error {
	if !c.force {
		ok, err := askConfirmation(fmt.Sprintf("Really delete context %q", c.name), false)
		if err != nil {
			return fmt.Errorf("could not obtain confirmation: %s", err)
		}

		if !ok {
			return nil
		}
	}

	return natscontext.DeleteContext(c.name)
}

func (c *ctxCommand) selectCommand(_ *kingpin.ParseContext) error {
	known := natscontext.KnownContexts()

	if len(known) == 0 {
		return fmt.Errorf("no context defined")
	}

	if c.name == "" {
		err := survey.AskOne(&survey.Select{
			Message: "Select a Context",
			Options: known,
		}, &c.name)
		if err != nil {
			return err
		}
	}

	if c.name == "" {
		return fmt.Errorf("please select a context to activate")
	}

	return natscontext.SelectContext(c.name)
}

func (c *ctxCommand) showIfNotEmpty(format string, arg string) {
	if arg == "" {
		return
	}

	fmt.Printf(format, arg)
}
