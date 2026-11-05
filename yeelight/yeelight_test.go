package yeelight

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lsongdev/ssdp-go/ssdp"
)

// fakeBulb serves one reply function per connection and records commands.
func fakeBulb(t *testing.T, reply func(conn net.Conn, cmd Command)) (*Yeelight, <-chan Command) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	commands := make(chan Command, 16)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var cmd Command
				if err := json.Unmarshal(line, &cmd); err != nil {
					return
				}
				commands <- cmd
				reply(conn, cmd)
			}()
		}
	}()
	addr := listener.Addr().(*net.TCPAddr)
	return New(&Config{Host: "127.0.0.1", Port: addr.Port, Timeout: 2 * time.Second}), commands
}

func writeResult(conn net.Conn, id int, result ...string) {
	body, _ := json.Marshal(map[string]interface{}{"id": id, "result": result})
	conn.Write(append(body, '\r', '\n'))
}

func ok(conn net.Conn, cmd Command) { writeResult(conn, cmd.ID, "ok") }

func TestCallSkipsNotificationsAndOtherIDs(t *testing.T) {
	y, _ := fakeBulb(t, func(conn net.Conn, cmd Command) {
		conn.Write([]byte(`{"method":"props","params":{"power":"on"}}` + "\r\n"))
		writeResult(conn, cmd.ID+1000, "stale")
		writeResult(conn, cmd.ID, "ok")
	})
	result, err := y.Toggle()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Result, []interface{}{"ok"}) {
		t.Fatalf("result = %v", result.Result)
	}
}

func TestCommandError(t *testing.T) {
	y, _ := fakeBulb(t, func(conn net.Conn, cmd Command) {
		conn.Write([]byte(`{"id":-1,"error":{"code":-1,"message":"client quota exceeded"}}` + "\r\n"))
	})
	_, err := y.Toggle()
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Message != "client quota exceeded" {
		t.Fatalf("err = %v", err)
	}
}

func TestCallDeviceClosesWithoutResult(t *testing.T) {
	y, _ := fakeBulb(t, func(net.Conn, Command) {})
	if _, err := y.Toggle(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSilentDeviceHonorsTimeoutAndContext(t *testing.T) {
	release := make(chan struct{})
	y, _ := fakeBulb(t, func(net.Conn, Command) { <-release })
	defer close(release)

	y.config.Timeout = 50 * time.Millisecond
	if _, err := y.Toggle(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout err = %v", err)
	}

	y.config.Timeout = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := y.Call(ctx, "toggle"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err = %v", err)
	}
}

func TestCommandParams(t *testing.T) {
	y, commands := fakeBulb(t, ok)
	smooth := &Effect{Effect: EffectSmooth, Duration: 500}
	for _, tc := range []struct {
		name   string
		call   func() (*CommandResult, error)
		method string
		params []interface{}
	}{
		{"nil effect", func() (*CommandResult, error) { return y.SetBright(50, nil) }, "set_bright", []interface{}{50.0, "sudden", 30.0}},
		{"power normal omits mode", func() (*CommandResult, error) { return y.SetPower("on", smooth, Normal) }, "set_power", []interface{}{"on", "smooth", 500.0}},
		{"power mode", func() (*CommandResult, error) { return y.SetPower("on", smooth, RGB) }, "set_power", []interface{}{"on", "smooth", 500.0, 2.0}},
		{"rgb", func() (*CommandResult, error) { return y.SetRGB(0xffffff, smooth) }, "set_rgb", []interface{}{16777215.0, "smooth", 500.0}},
		{"hsv", func() (*CommandResult, error) { return y.SetHSV(359, 100, smooth) }, "set_hsv", []interface{}{359.0, 100.0, "smooth", 500.0}},
		{"ct", func() (*CommandResult, error) { return y.SetCT(1700, smooth) }, "set_ct_abx", []interface{}{1700.0, "smooth", 500.0}},
		{"call", func() (*CommandResult, error) { return y.Call(context.Background(), "set_scene", "ct", 3500, 50) }, "set_scene", []interface{}{"ct", 3500.0, 50.0}},
		{"toggle sends empty params", y.Toggle, "toggle", []interface{}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.call(); err != nil {
				t.Fatal(err)
			}
			cmd := <-commands
			if cmd.Method != tc.method || !reflect.DeepEqual(cmd.Params, tc.params) {
				t.Fatalf("got %s %#v, want %s %#v", cmd.Method, cmd.Params, tc.method, tc.params)
			}
		})
	}
}

func TestInvalidValuesAreNotSent(t *testing.T) {
	y, commands := fakeBulb(t, ok)
	for name, call := range map[string]func() (*CommandResult, error){
		"bright low":   func() (*CommandResult, error) { return y.SetBright(0, nil) },
		"bright high":  func() (*CommandResult, error) { return y.SetBright(101, nil) },
		"ct":           func() (*CommandResult, error) { return y.SetCT(1699, nil) },
		"rgb":          func() (*CommandResult, error) { return y.SetRGB(0x1000000, nil) },
		"hue":          func() (*CommandResult, error) { return y.SetHSV(360, 0, nil) },
		"saturation":   func() (*CommandResult, error) { return y.SetHSV(0, 101, nil) },
		"state":        func() (*CommandResult, error) { return y.SetPower("toggle", nil, Normal) },
		"mode":         func() (*CommandResult, error) { return y.SetPower("on", nil, PowerMode(6)) },
		"effect":       func() (*CommandResult, error) { return y.SetBright(1, &Effect{Effect: "fade", Duration: 500}) },
		"zero effect":  func() (*CommandResult, error) { return y.SetBright(1, &Effect{}) },
		"short smooth": func() (*CommandResult, error) { return y.SetBright(1, &Effect{Effect: EffectSmooth, Duration: 29}) },
	} {
		if _, err := call(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	select {
	case cmd := <-commands:
		t.Fatalf("sent %+v", cmd)
	default:
	}
}

func TestGetProps(t *testing.T) {
	y, _ := fakeBulb(t, func(conn net.Conn, cmd Command) {
		conn.Write([]byte(`{"id":` + itoa(cmd.ID) + `,"result":["on","",16711680]}` + "\r\n"))
	})
	props, err := y.GetProps("power", "ct", "rgb")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"power": "on", "ct": "", "rgb": "16711680"}
	if !reflect.DeepEqual(props, want) {
		t.Fatalf("props = %v", props)
	}
	if _, err := y.GetProps("power"); err == nil {
		t.Fatal("expected error on value count mismatch")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestNewAppliesDefaultsWithoutMutatingConfig(t *testing.T) {
	config := &Config{Host: "fe80::1"}
	y := New(config)
	if config.Port != 0 || config.Timeout != 0 {
		t.Fatalf("config mutated: %+v", config)
	}
	if y.GetPort() != DefaultPort || y.config.Timeout != DefaultTimeout {
		t.Fatalf("defaults not applied: %+v", y.config)
	}
	if got := y.config.Address(); got != "[fe80::1]:55443" {
		t.Fatalf("Address() = %q", got)
	}
}

func TestLightsFromResponses(t *testing.T) {
	packet := func(pairs ...string) ssdp.Packet {
		header := http.Header{}
		for i := 0; i < len(pairs); i += 2 {
			header.Set(pairs[i], pairs[i+1])
		}
		return ssdp.Packet{Header: header}
	}
	lights := lightsFromResponses([]ssdp.Packet{
		packet("Location", "yeelight://192.168.1.2:55443", "id", "0x1"),
		packet("Location", "yeelight://192.168.1.2:55443", "id", "0x1"),
		packet("Location", "yeelight://192.168.1.9:55443", "id", "0x1"), // same device, new address
		packet("Location", "yeelight://192.168.1.3:1234"),
		packet("Location", "yeelight://192.168.1.4"),
		packet("Location", "http://192.168.1.5:80/desc.xml"),
		packet("Location", "yeelight://192.168.1.6:notaport"),
		packet("Location", ":bad"),
		packet(),
	})
	var got []string
	for _, light := range lights {
		got = append(got, light.config.Address())
	}
	want := []string{"192.168.1.2:55443", "192.168.1.3:1234", "192.168.1.4:55443"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// fakeResponder answers M-SEARCH like a bulb, over loopback unicast.
func fakeResponder(t *testing.T) ssdp.Config {
	t.Helper()
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, remote, err := server.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if strings.Contains(string(buf[:n]), "ST: "+searchTarget) {
				server.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nLocation: yeelight://127.0.0.1:55443\r\nid: 0x1\r\n\r\n"), remote)
			}
		}
	}()
	return ssdp.Config{Port: server.LocalAddr().(*net.UDPAddr).Port, Address: "127.0.0.1"}
}

func TestDiscoverKeepsDevicesWhenDeadlineEndsSearch(t *testing.T) {
	config := fakeResponder(t)
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		lights, err := discover(ctx, config)
		cancel()
		if err != nil || len(lights) != 1 {
			t.Fatalf("run %d: lights=%v err=%v", i, lights, err)
		}
	}
}

func TestDiscoverReturnsDevicesFoundBeforeCancel(t *testing.T) {
	config := fakeResponder(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	lights, err := discover(ctx, config)
	if !errors.Is(err, context.Canceled) || len(lights) != 1 {
		t.Fatalf("lights=%v err=%v", lights, err)
	}
}

func TestDiscoverExpiredContext(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := discover(ctx, fakeResponder(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestFindReturnsFirstDeviceWithoutWaiting(t *testing.T) {
	config := fakeResponder(t)
	config.Window = time.Minute
	start := time.Now()
	light, err := find(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if got := light.config.Address(); got != "127.0.0.1:55443" {
		t.Fatalf("address = %q", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v", elapsed)
	}
}

func TestFindWithoutDevices(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	config := ssdp.Config{Port: server.LocalAddr().(*net.UDPAddr).Port, Address: "127.0.0.1", Window: 20 * time.Millisecond}
	if _, err := find(context.Background(), config); !errors.Is(err, ErrNoDevices) {
		t.Fatalf("err = %v", err)
	}
}

func TestFindCanceled(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	config := ssdp.Config{Port: server.LocalAddr().(*net.UDPAddr).Port, Address: "127.0.0.1"}
	if _, err := find(ctx, config); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
