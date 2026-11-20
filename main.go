// SPDX-License-Identifier: GPL-2.0-only
// Copyright (C) 2026, Shu De Zheng <imchuncai@gmail.com>. All Rights Reserved.

package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	client "github.com/imchuncai/umem-cache-client-Go"
	"github.com/imchuncai/umem-cache-client-Go/proto"
)

func parseConfig() (client.Config, error) {
	timeout := flag.Int("timeout", 2, "timeout seconds, default 2 seconds")
	tlsKey := flag.String("tls-key", "", `tls key (default "")`)
	tlsCert := flag.String("tls-cert", "", `tls cert (default "")`)
	tlsCaCert := flag.String("tls-ca-cert", "", `tls ca cert (default "")`)
	flag.Parse()

	tlsFull := *tlsKey != "" && *tlsCert != "" && *tlsCaCert != ""
	tlsEmpty := *tlsKey == "" && *tlsCert == "" && *tlsCaCert == ""
	if tlsFull == tlsEmpty {
		return client.Config{}, errors.New("bad tls config")
	}

	var tlsConfig *tls.Config
	if tlsFull {
		cert, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
		if err != nil {
			return client.Config{}, fmt.Errorf("load tls_key and tls_cert failed: %w", err)
		}

		caCert, err := os.ReadFile(*tlsCaCert)
		if err != nil {
			return client.Config{}, fmt.Errorf("load tls_ca_cert failed: %w", err)
		}

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		}
	}
	return client.Config{time.Duration(*timeout) * time.Second, 0, tlsConfig}, nil
}

func usage() {
	fmt.Printf(`
Usage:
  umem-cache-ctl [-tls_key <path> -tls_cert <path> -tls_ca_cert <path>]
                 [-timeout <seconds>] <init|change|cluster|stat|stats|warmup>
		 <address...>

Example:
  umem-cache-ctl stat [::1]:10047

WARNING:
  The warmup command is stupid, don't use it unless you know what you're doing.
`)
}

func printError(err error) {
	fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s\n", err.Error())
}

func __cluster(addresses []string, config client.Config) (client.Cluster, error) {
	c, err := client.NewCluster(addresses, config)
	if err != nil {
		return client.Cluster{}, err
	}

	return c.Cluster()
}

func stats(clients []*client.Client) []proto.Stats {
	all := make([]proto.Stats, 0, len(clients))
	for _, client := range clients {
		all = append(all, client.Stats()...)
	}
	return all
}

func align(list []string) []string {
	var max int
	for _, v := range list {
		if len(v) > max {
			max = len(v)
		}
	}

	ret := make([]string, len(list))
	for i, v := range list {
		ret[i] = strings.Repeat(" ", max-len(v)) + v
	}
	return ret
}

func __printStats(now time.Time, nodes int, old, new []proto.Stats) {
	var alive, connects, disconnects, hits, misses, dels, deltaHits, deltaMisses, deltaDels uint64
	for i := range old {
		if new[i].Error != nil {
			continue
		}
		alive++
		if old[i].Error != nil {
			continue
		}

		connects += new[i].Connects
		disconnects += new[i].Disconnects

		hits += new[i].Hits
		misses += new[i].Misses
		dels += new[i].Dels

		delta := new[i].StatNanoseconds - old[i].StatNanoseconds
		deltaHits += ((new[i].Hits-old[i].Hits)*1000000000*10/delta + 5) / 10
		deltaMisses += ((new[i].Misses-old[i].Misses)*1000000000*10/delta + 5) / 10
		deltaDels += ((new[i].Dels-old[i].Dels)*1000000000*10/delta + 5) / 10
	}
	gets := hits + misses
	deltaGets := deltaHits + deltaMisses

	c1 := align([]string{
		strconv.FormatUint(gets, 10),
		strconv.FormatUint(hits, 10),
		strconv.FormatUint(dels, 10),
		fmt.Sprintf("   %.02f%%", float64(hits)/float64(gets)),
	})
	c2 := align([]string{
		strconv.FormatUint(deltaGets, 10) + "/s",
		strconv.FormatUint(deltaHits, 10) + "/s",
		strconv.FormatUint(deltaDels, 10) + "/s",
		fmt.Sprintf("%.02f%%", float64(deltaHits*100)/float64(deltaGets)),
	})
	c3 := align([]string{
		strconv.FormatUint(connects, 10),
		strconv.FormatUint(disconnects, 10),
		strconv.FormatUint(connects-disconnects, 10),
	})
	fmt.Printf("\033[%dA"+
		"\033[2Kstats - %s,   %d nodes,   %d / %d workers\n"+
		"\033[2Kgets:    %s   %s      connects:   %s\n"+
		"\033[2Khits:    %s   %s   disconnects:   %s\n"+
		"\033[2Kdels:    %s   %s   connections:   %s\n"+
		"\033[2Khit_rate:%s   %s\n",
		5,
		now.Format("15:04:05"), nodes, alive, len(old),
		c1[0], c2[0], c3[0],
		c1[1], c2[1], c3[1],
		c1[2], c2[2], c3[2],
		c1[3], c2[3],
	)
}

func printStats(clients []*client.Client) {
	fmt.Printf("\n\n\n\n\n")
	old := stats(clients)
	ticker := time.NewTicker(2 * time.Second)
	for {
		now := <-ticker.C
		new := stats(clients)
		__printStats(now, len(clients), old, new)
		old = new
	}
}

func communicate(conn net.Conn, req []byte, res []byte) error {
	_, err := conn.Write(req)
	if err != nil {
		return fmt.Errorf("Write failed: %w", err)
	}

	_, err = conn.Read(res)
	if err != nil {
		return fmt.Errorf("Read failed: %w", err)
	}

	return nil
}

func __set(deadline time.Time, address string, size uint64, config *tls.Config) (bool, error) {
	var err error
	var conn net.Conn
	tcpDialer := net.Dialer{Deadline: deadline}
	if config == nil {
		conn, err = tcpDialer.Dial("tcp6", address)
	} else {
		dialer := tls.Dialer{Config: config, NetDialer: &tcpDialer}
		conn, err = dialer.Dial("tcp6", address)
	}
	if err != nil {
		return false, fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	res := make([]byte, 1)
	err = communicate(conn, make([]byte, 8), res)
	if err != nil {
		return false, fmt.Errorf("connect thread 0 failed: %w", err)
	}

	req := make([]byte, 2)
	req[0] = 1
	err = communicate(conn, req, res)
	if err != nil {
		return false, fmt.Errorf("Del failed: %w", err)
	}

	req[0] = 0
	res = make([]byte, 9)
	err = communicate(conn, req, res)
	if err != nil {
		return false, fmt.Errorf("Get failed: %w", err)
	}
	if res[8] == 1 {
		return false, nil
	}

	req = make([]byte, 8)
	binary.LittleEndian.PutUint64(req, size)
	_, err = conn.Write(req)
	if err != nil {
		return false, fmt.Errorf("Set failed: %w", err)
	}

	return true, nil
}

func warmup(deadline time.Time, address string, config client.Config) error {
	cli, err := client.New(address, config)
	if err != nil {
		return fmt.Errorf("new client failed: %w", err)
	}
	defer cli.Close()

	stats := cli.Stats()
	if stats[0].Error != nil {
		return fmt.Errorf("get stats failed: %w", stats[0].Error)
	}

	size := stats[0].ConfigMemLimit / uint64(stats[0].ConfigThreadNR)
	for {
		ok, err := __set(deadline, address, size, config.TLSConfig)
		if err != nil {
			return err
		}

		if ok {
			return nil
		}
	}
}

func main() {
	flag.Usage = usage

	config, err := parseConfig()
	if err != nil {
		printError(err)
		return
	}

	args := flag.Args()
	if len(args) <= 0 {
		usage()
		return
	}

	deadline := time.Now().Add(config.Timeout)
	addresses := args[1:]
	if len(addresses) == 0 {
		usage()
		return
	}

	switch args[0] {
	case "init":
		err := client.AdminInitCluster(deadline, addresses, config.TLSConfig)
		if err != nil {
			printError(err)
			return
		}
		fmt.Println("Success!")
	case "change":
		err := client.AdminChangeCluster(deadline, addresses, config.TLSConfig)
		if err != nil {
			printError(err)
			return
		}
		fmt.Println("Success! But you still have to confirm it using the `cluster` command.")
	case "cluster":
		cluster, err := __cluster(addresses, config)
		if err != nil {
			printError(fmt.Errorf("get cluster failed: %w", err))
			return
		}

		fmt.Printf("type: %v, version: %d, leader: %s\n", cluster.Type, cluster.Version, cluster.Leader.String())
		for _, m := range cluster.Machines {
			fmt.Printf("%s id: %d stability: %d version: %d available: %v\n",
				m.Addr.String(), m.ID, m.Stability, m.Version, m.Available())
		}
	case "stat":
		c, err := client.New(addresses[0], config)
		if err != nil {
			printError(err)
			return
		}
		clients := []*client.Client{c}
		printStats(clients)
	case "stats":
		cluster, err := __cluster(addresses, config)
		if err != nil {
			printError(fmt.Errorf("get cluster failed: %w", err))
			return
		}

		clients := make([]*client.Client, len(cluster.Machines))
		for i, m := range cluster.Machines {
			clients[i], err = client.New(m.Addr.String(), config)
			if err != nil {
				printError(fmt.Errorf("new client: %s failed: %w", m.Addr.String(), err))
				return
			}
		}

		printStats(clients)
	case "warmup":
		for _, address := range addresses {
			err := warmup(deadline, address, config)
			if err != nil {
				printError(fmt.Errorf("warmup %s failed: %w", address, err))
				return
			}
		}
		fmt.Println("Success!")
	default:
		printError(fmt.Errorf("Unknown command: %s", args[0]))
	}
}
