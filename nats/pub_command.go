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
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/alecthomas/kingpin.v2"
)

type pubCmd struct {
	subject   string
	body      string
	req       bool
	replyTo   string
	respCount uint
	raw       bool
	hdrs      []string
}

func configurePubCommand(app *kingpin.Application) {
	c := &pubCmd{}
	pub := app.Command("pub", "Generic data publishing utility").Action(c.publish)
	pub.Arg("subject", "Subject to subscribe to").Required().StringVar(&c.subject)
	pub.Arg("body", "Message body").Default("!nil!").StringVar(&c.body)
	pub.Flag("wait", "Wait for a reply from a service").Short('w').BoolVar(&c.req)
	pub.Flag("reply", "Sets a custom reply to subject").StringVar(&c.replyTo)
	pub.Flag("header", "Adds headers to the message").Short('H').StringsVar(&c.hdrs)

	req := app.Command("request", "Generic data request utility").Alias("req").Action(c.publish)
	req.Arg("subject", "Subject to subscribe to").Required().StringVar(&c.subject)
	req.Arg("body", "Message body").Default("!nil!").StringVar(&c.body)
	req.Flag("wait", "Wait for a reply from a service").Short('w').Default("true").Hidden().BoolVar(&c.req)
	req.Flag("raw", "Show just the output received").Short('r').Default("false").BoolVar(&c.raw)
	req.Flag("header", "Adds headers to the message").Short('H').StringsVar(&c.hdrs)
	req.Flag("reply_count", "Number of expected replies (within timeout)").Short('c').Default("1").UintVar(&c.respCount)
}

func (c *pubCmd) prepareMsg() (*nats.Msg, error) {
	msg := nats.NewMsg(c.subject)
	msg.Reply = c.replyTo
	msg.Data = []byte(c.body)

	return msg, parseStringsToHeader(c.hdrs, msg)
}

func (c *pubCmd) printResponse(m *nats.Msg) {
	if !c.raw {
		log.Printf("Received on %q", m.Subject)
		if len(m.Header) > 0 {
			for h, vals := range m.Header {
				for _, val := range vals {
					log.Printf("%s: %s", h, val)
				}
			}

			fmt.Println()
		}
	}

	fmt.Println(string(m.Data))

	if !c.raw && !strings.HasSuffix(string(m.Data), "\n") {
		fmt.Println()
	}
}

func (c *pubCmd) publish(pc *kingpin.ParseContext) error {
	nc, err := newNatsConn(servers, natsOpts()...)
	if err != nil {
		return err
	}
	defer nc.Close()

	if c.body == "!nil!" && terminal.IsTerminal(int(os.Stdout.Fd())) {
		log.Println("Reading payload from STDIN")
		body, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		c.body = string(body)
	}

	if c.req {
		if !c.raw {
			log.Printf("Sending request on %q\n", c.subject)
		}

		msg, err := c.prepareMsg()
		if err != nil {
			return err
		}

		if c.respCount == 1 {
			m, err := nc.RequestMsg(msg, timeout)
			if err != nil {
				return err
			}
			c.printResponse(m)
		} else {
			if msg.Reply == "" {
				msg.Reply = nc.NewRespInbox()
			}

			msgChan := make(chan *nats.Msg, c.respCount)
			defer close(msgChan)
			sub, err := nc.ChanSubscribe(msg.Reply, msgChan)
			if err != nil {
				return err
			}
			defer sub.Drain()

			err = nc.PublishMsg(msg)
			if err != nil {
				return err
			}

			cnt := uint(0)
			t := time.NewTimer(timeout)
			for {
				select {
				case <-t.C:
					return nil
				case m := <-msgChan:
					c.printResponse(m)
					cnt++
					if cnt == c.respCount {
						return nil
					}
				}
			}

		}
		return nil
	}

	msg, err := c.prepareMsg()
	if err != nil {
		return err
	}

	err = nc.PublishMsg(msg)
	if err != nil {
		return err
	}
	nc.Flush()

	err = nc.LastError()
	if err != nil {
		return err
	}

	log.Printf("Published %d bytes to %q\n", len(c.body), c.subject)

	return nil
}
