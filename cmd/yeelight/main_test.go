package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
)

type command struct {
	ID     int           `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// fakeBulb sends a notification ahead of every result, as a real device can.
func fakeBulb(t *testing.T, result string) (port string, commands <-chan command) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan command, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			var cmd command
			json.Unmarshal(line, &cmd)
			received <- cmd
			fmt.Fprintf(conn, "{\"method\":\"props\",\"params\":{\"power\":\"on\"}}\r\n{\"id\":%d,\"result\":%s}\r\n", cmd.ID, result)
			conn.Close()
		}
	}()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), received
}

func TestFlagPositions(t *testing.T) {
	for name, build := range map[string]func(host, port string) []string{
		"after":   func(h, p string) []string { return []string{"power", "on", h, p} },
		"between": func(h, p string) []string { return []string{"power", h, "on", p} },
		"before":  func(h, p string) []string { return []string{h, p, "power", "on"} },
	} {
		t.Run(name, func(t *testing.T) {
			port, commands := fakeBulb(t, `["ok"]`)
			var stdout, stderr bytes.Buffer
			code := run(build("--host=127.0.0.1", "--port="+port), &stdout, &stderr)
			if code != exitOK {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			if cmd := <-commands; cmd.Method != "set_power" || cmd.Params[0] != "on" {
				t.Fatalf("command = %+v", cmd)
			}
			if !strings.Contains(stdout.String(), "turned on") {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}

func TestStatusUsesOneCommand(t *testing.T) {
	port, commands := fakeBulb(t, `["on","80","2",""]`)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"status", "--host=127.0.0.1", "--port=" + port}, &stdout, &stderr); code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if cmd := <-commands; cmd.Method != "get_prop" || len(cmd.Params) != 4 {
		t.Fatalf("command = %+v", cmd)
	}
	if len(commands) != 0 {
		t.Fatalf("%d extra commands", len(commands))
	}
	for _, want := range []string{"Power status: on", "Brightness: 80%", "Color temperature: N/A"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout %q lacks %q", stdout.String(), want)
		}
	}
}

func TestDeviceErrorExitCode(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close() // connection refused
	var stdout, stderr bytes.Buffer
	if code := run([]string{"status", "--host=127.0.0.1", "--port=" + port}, &stdout, &stderr); code != exitError {
		t.Fatalf("code=%d", code)
	}
	if !strings.HasPrefix(stderr.String(), "Error: ") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// Host is deliberately omitted: a usage error must be reported before discovery.
func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"--nope"},
		{"--port=abc", "status"},
		{"status", "extra"},
		{"discover", "extra"},
		{"power"},
		{"power", "dim"},
		{"power", "on", "off"},
		{"brightness"},
		{"brightness", "abc"},
		{"brightness", "0"},
		{"brightness", "101"},
		{"temp", "1699"},
		{"temp", "hot"},
		{"color", "fffffffff"},
		{"color", "fff"},
		{"color", "-00001"},
		{"color", "gggggg"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("run(%q) = %d, want %d", args, code, exitUsage)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitOK || !strings.Contains(stdout.String(), "Usage:") {
			t.Errorf("run(%q) = %d, stdout %q", args, code, stdout.String())
		}
	}
}

func TestParseColor(t *testing.T) {
	for input, want := range map[string]int{"ff0000": 0xff0000, "#00FF00": 0x00ff00, "000000": 0} {
		if got, err := parseColor(input); err != nil || got != want {
			t.Errorf("parseColor(%q) = %x, %v", input, got, err)
		}
	}
}
